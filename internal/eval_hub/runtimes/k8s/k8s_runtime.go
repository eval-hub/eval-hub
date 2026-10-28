package k8s

// Runtime entrypoints for Kubernetes job creation.
import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
	"github.com/eval-hub/eval-hub/internal/eval_hub/constants"
	"github.com/eval-hub/eval-hub/internal/eval_hub/messages"
	"github.com/eval-hub/eval-hub/internal/eval_hub/metrics"
	"github.com/eval-hub/eval-hub/internal/eval_hub/runtimes/shared"
	"github.com/eval-hub/eval-hub/internal/eval_hub/serviceerrors"
	"github.com/eval-hub/eval-hub/internal/otel"
	"github.com/eval-hub/eval-hub/pkg/api"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

type K8sRuntime struct {
	logger        *slog.Logger
	serviceConfig *config.Config
	helper        *KubernetesHelper
	ctx           context.Context
	// Test hooks (nil in production). Used to pause/observe create vs delete races
	// without blocking inside fake-clientset reactors (those hold the clientset lock).
	beforeCreateBenchmarkResources func()
	afterCreateBenchmarkResources  func(err error)
	beforeSweepAfterAbort          func()
}

// orphanCleanupTimeout bounds K8s deletes that must succeed even when the
// caller's context (HTTP DELETE or cancelled create) is already done.
const orphanCleanupTimeout = 30 * time.Second

func orphanCleanupContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), orphanCleanupTimeout)
}

// NewK8sRuntime creates a Kubernetes runtime.
func NewK8sRuntime(logger *slog.Logger, serviceConfig *config.Config) (abstractions.Runtime, error) {
	helper, err := NewKubernetesHelper()
	if err != nil {
		return nil, err
	}
	return &K8sRuntime{
		logger:        logger,
		serviceConfig: serviceConfig,
		helper:        helper,
	}, nil
}

func (r *K8sRuntime) WithLogger(logger *slog.Logger) abstractions.Runtime {
	return &K8sRuntime{
		logger:        logger,
		serviceConfig: r.serviceConfig,
		helper:        r.helper,
		ctx:           r.ctx,
	}
}

func (r *K8sRuntime) WithContext(ctx context.Context) abstractions.Runtime {
	return &K8sRuntime{
		logger:        r.logger,
		serviceConfig: r.serviceConfig,
		helper:        r.helper,
		ctx:           ctx,
	}
}

// Close stops the Kubernetes EventBroadcaster background goroutines started by NewKubernetesHelper.
// WithLogger/WithContext copies share the same helper pointer, so Close must be called exactly once
// on the root runtime returned by NewK8sRuntime.
func (r *K8sRuntime) Close() error {
	if r.helper == nil {
		return nil
	}
	return r.helper.Close()
}

func (r *K8sRuntime) RunEvaluationJob(
	evaluation *api.EvaluationJobResource,
	benchmarks []api.EvaluationBenchmarkConfig,
	storage abstractions.RuntimeStorage,
) error {
	if len(benchmarks) == 0 {
		return serviceerrors.NewServiceError(messages.EvaluationJobEmpty, "EvaluationJobID", evaluation.Resource.ID)
	}

	jobID := evaluation.Resource.ID
	go func() {
		for idx, bench := range benchmarks {
			// Shared DB signal works across eval-hub replicas: hard_delete removes the
			// row, soft cancel sets cancelled — either stops further Job creation.
			if aborted, reason := evaluationCreateAborted(storage, jobID); aborted {
				r.logger.Info(
					"skipping remaining kubernetes job creation; evaluation deleted or cancelled; sweeping leftovers",
					"job_id", jobID,
					"benchmark_id", bench.ID,
					"benchmark_index", idx,
					"reason", reason,
				)
				// Retry cleanup if the cancel/delete handler's sweep failed or a prior
				// benchmark left resources behind before this pre-check ran.
				r.sweepEvaluationResourcesAfterAbort(evaluation, jobID, bench.ID)
				return
			}
			// DetachedContext keeps request-trace linkage without inheriting the
			// HTTP request deadline (see OTEL.md "Trace continuity").
			benchCtx := otel.DetachedContext(r.ctx)
			if r.beforeCreateBenchmarkResources != nil {
				r.beforeCreateBenchmarkResources()
			}
			err := r.createBenchmarkResources(benchCtx, r.logger, evaluation, &bench, idx, storage)
			if r.afterCreateBenchmarkResources != nil {
				r.afterCreateBenchmarkResources(err)
			}
			// After any create outcome: if storage already signalled delete/cancel,
			// sweep leftovers (success or uncertain/failed persist) and stop.
			if aborted, reason := evaluationCreateAborted(storage, jobID); aborted {
				r.logger.Info(
					"kubernetes job creation aborted; evaluation deleted or cancelled; sweeping leftovers",
					"job_id", jobID,
					"benchmark_id", bench.ID,
					"benchmark_index", idx,
					"reason", reason,
					"error", err,
				)
				r.sweepEvaluationResourcesAfterAbort(evaluation, jobID, bench.ID)
				return
			}
			if err != nil {
				metrics.RecordBenchmarkRuntimeError(benchCtx, r.Name(), evaluation.Resource.Tenant.String())
				r.logger.Error(
					"kubernetes job creation failed",
					"error", err,
					"job_id", jobID,
					"benchmark_id", bench.ID,
				)
				if storage != nil {
					runStatus := buildBenchmarkFailureStatus(&bench, idx, err)
					if updateErr := storage.UpdateEvaluationJob(jobID, runStatus); updateErr != nil {
						r.logger.Error(
							"failed to update benchmark status",
							"error", updateErr,
							"job_id", jobID,
							"benchmark_id", bench.ID,
						)
					}
				}
			}
		}
	}()
	return nil
}

// sweepEvaluationResourcesAfterAbort is the create-loop counterpart to hard_delete
// cleanup: reuse DeleteEvaluationJobResources so new resource types only need one delete path.
func (r *K8sRuntime) sweepEvaluationResourcesAfterAbort(evaluation *api.EvaluationJobResource, jobID, benchmarkID string) {
	if r.beforeSweepAfterAbort != nil {
		r.beforeSweepAfterAbort()
	}
	if cleanupErr := r.DeleteEvaluationJobResources(evaluation); cleanupErr != nil {
		r.logger.Error(
			"failed to sweep kubernetes resources created after evaluation delete/cancel",
			"error", cleanupErr,
			"job_id", jobID,
			"benchmark_id", benchmarkID,
		)
	}
}

// evaluationCreateAborted reports whether create should stop because the evaluation
// was hard-deleted or soft-cancelled in shared storage (visible to every replica).
func evaluationCreateAborted(storage abstractions.RuntimeStorage, jobID string) (bool, string) {
	if storage == nil {
		return false, ""
	}
	job, err := storage.GetEvaluationJob(jobID)
	if err != nil {
		var serviceErr *serviceerrors.ServiceError
		if errors.As(err, &serviceErr) && serviceErr.MessageCode() == messages.ResourceNotFound {
			return true, "evaluation job deleted"
		}
		// Transient storage errors must not abort an in-progress create loop.
		return false, ""
	}
	if job == nil {
		return true, "evaluation job deleted"
	}
	if job.Status != nil && job.Status.State == api.OverallStateCancelled {
		return true, "evaluation job cancelled"
	}
	return false, ""
}

// jobForegroundDeleteOptions deletes Job-owned Pods before removing the Job so stuck Init
// pods cannot outlive a background Job delete during hard_delete or orphan cleanup.
func jobForegroundDeleteOptions() metav1.DeleteOptions {
	propagationPolicy := metav1.DeletePropagationForeground
	return metav1.DeleteOptions{PropagationPolicy: &propagationPolicy}
}

func (r *K8sRuntime) DeleteEvaluationJobResources(evaluation *api.EvaluationJobResource) error {
	jobID := evaluation.Resource.ID
	namespace := resolveNamespace(string(evaluation.Resource.Tenant))
	deleteOptions := jobForegroundDeleteOptions()

	r.logger.Info(
		"deleting evaluation runtime resources",
		"job_id", jobID,
		"benchmark_count", len(evaluation.Benchmarks),
		"namespace", namespace,
	)

	labelSelector := fmt.Sprintf("%s=%s", labelJobIDKey, sanitizeLabelValue(jobID))
	// Two sweeps with independent timeouts: storage was signalled first so create
	// loops stop; the second list catches Jobs created between sweeps.
	// Delete Jobs and ref secrets. When no labeled Jobs remain, also delete labeled
	// ConfigMaps that have no Job owner (partial-create leftovers). Job-owned
	// ConfigMaps/NetworkPolicies are left to Kubernetes GC. We never delete ConfigMaps
	// while a labeled Job still exists — that would GC ConfigMap-owned NetworkPolicies
	// off running Pods during the Job-create window before SetConfigMapOwner.
	cleanupSweep := func() error {
		cleanupCtx, cancelCleanup := orphanCleanupContext()
		defer cancelCleanup()
		return r.deleteLabeledJobsAndRefSecrets(cleanupCtx, jobID, namespace, labelSelector, deleteOptions)
	}
	return errors.Join(cleanupSweep(), cleanupSweep())
}

func (r *K8sRuntime) deleteLabeledJobsAndRefSecrets(
	ctx context.Context,
	jobID, namespace, labelSelector string,
	deleteOptions metav1.DeleteOptions,
) error {
	jobs, err := r.helper.ListJobs(ctx, namespace, labelSelector)
	if err != nil {
		return err
	}
	var deleteErr error
	for _, job := range jobs {
		r.logger.Info(
			"deleting evaluation runtime job",
			"job_id", jobID,
			"job_name", job.Name,
			"namespace", namespace,
		)
		if err := r.helper.DeleteJob(ctx, namespace, job.Name, deleteOptions); err != nil && !apierrors.IsNotFound(err) {
			deleteErr = errors.Join(deleteErr, err)
		}
	}
	// Delete ref secrets explicitly so they are never orphaned even if Job owner-ref
	// GC is delayed or the owner ref was never set.
	secrets, err := r.helper.ListSecrets(ctx, namespace, labelSelector)
	if err != nil {
		deleteErr = errors.Join(deleteErr, err)
	}
	for _, secret := range secrets {
		r.logger.Info(
			"deleting evaluation runtime ref secret",
			"job_id", jobID,
			"secret_name", secret.Name,
			"namespace", namespace,
		)
		if err := r.helper.DeleteSecret(ctx, namespace, secret.Name, deleteOptions); err != nil && !apierrors.IsNotFound(err) {
			deleteErr = errors.Join(deleteErr, err)
		}
	}
	// Foreground Job deletion leaves Jobs listed until Pods are gone. Skip ConfigMap
	// cleanup while any remain so we do not strip NetworkPolicy from running Pods.
	remainingJobs, listErr := r.helper.ListJobs(ctx, namespace, labelSelector)
	if listErr != nil {
		return errors.Join(deleteErr, listErr)
	}
	if len(remainingJobs) > 0 {
		return deleteErr
	}
	return errors.Join(deleteErr, r.deleteUnownedLabeledConfigMaps(ctx, jobID, namespace, labelSelector))
}

func configMapHasJobOwner(cm corev1.ConfigMap) bool {
	for _, owner := range cm.OwnerReferences {
		if owner.Kind == "Job" {
			return true
		}
	}
	return false
}

// deleteUnownedLabeledConfigMaps removes labeled ConfigMaps that were never adopted by a
// Job (partial / uncertain create). Job-owned ConfigMaps are left to Kubernetes GC.
func (r *K8sRuntime) deleteUnownedLabeledConfigMaps(ctx context.Context, jobID, namespace, labelSelector string) error {
	configMaps, err := r.helper.ListConfigMaps(ctx, namespace, labelSelector)
	if err != nil {
		return err
	}
	var deleteErr error
	for _, cm := range configMaps {
		if configMapHasJobOwner(cm) {
			continue
		}
		r.logger.Info(
			"deleting evaluation runtime configmap",
			"job_id", jobID,
			"configmap_name", cm.Name,
			"namespace", namespace,
		)
		if err := r.helper.DeleteConfigMap(ctx, namespace, cm.Name); err != nil && !apierrors.IsNotFound(err) {
			deleteErr = errors.Join(deleteErr, err)
		}
	}
	return deleteErr
}

func (r *K8sRuntime) createBenchmarkResources(ctx context.Context,
	logger *slog.Logger,
	evaluation *api.EvaluationJobResource,
	benchmark *api.EvaluationBenchmarkConfig,
	benchmarkIndex int,
	storage abstractions.RuntimeStorage,
) error {
	benchmarkID := benchmark.ID
	// Provider/benchmark validation should be handled during creation.
	provider, err := shared.ProviderForBenchmark(
		evaluation,
		*benchmark,
		storage,
	)
	if err != nil {
		return err
	}
	var hardwareProfile *hardwareProfileResources
	effectiveHW := api.EffectiveHardwareConfig(benchmark, &evaluation.EvaluationJobConfig)
	if effectiveHW != nil {
		profileName := strings.TrimSpace(effectiveHW.HardwareProfileName)
		if profileName != "" {
			// Create already validated existence/disabled via ValidateHardwareProfiles.
			// Re-fetch here only to apply resources and scheduling to the Job.
			profileNamespace, err := hardwareProfilesNamespace()
			if err != nil {
				logger.Error(
					"hardware profiles namespace is not configured",
					"error", err,
					"env", hardwareProfilesNamespaceEnv,
					"job_id", evaluation.Resource.ID,
					"benchmark_id", benchmarkID,
					"profile", profileName,
				)
				return fmt.Errorf("job %s benchmark %s: %w", evaluation.Resource.ID, benchmarkID, err)
			}
			profileCR, err := r.helper.GetHardwareProfile(ctx, profileNamespace, profileName)
			if err != nil {
				return fmt.Errorf("job %s benchmark %s: fetch hardware profile %q in namespace %q: %w",
					evaluation.Resource.ID, benchmarkID, profileName, profileNamespace, err)
			}
			parsed, err := parseHardwareProfileResources(profileCR)
			if err != nil {
				return fmt.Errorf("job %s benchmark %s: parse hardware profile %q: %w", evaluation.Resource.ID, benchmarkID, profileName, err)
			}
			hardwareProfile = parsed
		}
	}
	jobConfig, err := buildJobConfig(evaluation, provider, benchmark, benchmarkIndex, r.serviceConfig, hardwareProfile)
	if err != nil {
		logger.Error("kubernetes job config error", "benchmark_id", benchmarkID, "error", err)
		return fmt.Errorf("job %s benchmark %s: %w", evaluation.Resource.ID, benchmarkID, err)
	}
	if r.serviceConfig == nil || r.serviceConfig.Service == nil {
		return fmt.Errorf("service config is required")
	}
	jobConfig.testDataInitImage = r.serviceConfig.Service.EvalInitImage
	jobConfig.mlflowCABundleConfigMap = r.resolveMLFlowCABundleConfigMap(ctx, jobConfig, logger)
	logger.Info(
		"kubernetes job config",
		"job_id", evaluation.Resource.ID,
		"benchmark_id", benchmarkID,
		"service_account", jobConfig.serviceAccountName,
		"service_ca_configmap", jobConfig.serviceCAConfigMap,
		"mlflow_ca_bundle_configmap", jobConfig.mlflowCABundleConfigMap,
		"eval_hub_url", jobConfig.evalHubURL,
	)
	// When a model URL is present, redirect the adapter through the sidecar model proxy so
	// all model traffic (open and authenticated) flows through the sidecar. For pre-recorded-data
	// jobs the model URL is empty and the entire model proxy / auth pipeline is skipped.
	var secretInfo modelSecretInfo
	if jobConfig.modelTargetURL != "" {
		rewrittenModelURL, err := rewriteModelURLForSidecar(jobConfig.sidecarBaseURL, jobConfig.modelTargetURL)
		if err != nil {
			return fmt.Errorf("job %s benchmark %s: rewriting model URL for sidecar: %w", evaluation.Resource.ID, benchmarkID, err)
		}
		jobConfig.jobSpec.Model.URL = rewrittenModelURL

		if jobConfig.modelAuthSecretRef != "" {
			secretInfo, err = inspectModelSecret(ctx, jobConfig.namespace, jobConfig.modelAuthSecretRef, r.helper)
			if err != nil {
				logger.Error("kubernetes model secret inspect error", "benchmark_id", benchmarkID, "error", err)
				return fmt.Errorf("job %s benchmark %s: reading model auth secret: %w", evaluation.Resource.ID, benchmarkID, err)
			}
			if secretInfo.hasCredentialKeys {
				jobConfig.modelInternalRefSecretName = buildK8sName(jobConfig.jobID, jobConfig.resourceGUID, "-model-ref")
			} else {
				logger.Info("model credential secret has no proxy-injectable keys; sidecar proxy active for SA token")
			}
		}
	}
	// Build sidecar config after inspecting the model secret so modelInternalRefSecretName is set.
	sidecarJSON, err := sidecarForJobPod(r.serviceConfig, jobConfig)
	if err != nil {
		return fmt.Errorf("job %s benchmark %s: sidecar config json: %w", evaluation.Resource.ID, benchmarkID, err)
	}
	jobConfig.sidecarConfig = sidecarJSON

	configMap, err := buildConfigMap(jobConfig)
	if err != nil {
		logger.Error("kubernetes configmap build error", "benchmark_id", benchmarkID, "error", err)
		return fmt.Errorf("job %s benchmark %s: %w", evaluation.Resource.ID, benchmarkID, err)
	}
	job, err := buildJob(jobConfig, r.serviceConfig)
	if err != nil {
		logger.Error("kubernetes job build error", "benchmark_id", benchmarkID, "error", err)
		return fmt.Errorf("job %s benchmark %s: %w", evaluation.Resource.ID, benchmarkID, err)
	}
	networkPolicy, err := buildJobNetworkPolicy(jobConfig)
	if err != nil {
		return fmt.Errorf("job %s benchmark %s: network policy: %w", evaluation.Resource.ID, benchmarkID, err)
	}
	hasServiceCAVolume := false
	for _, volume := range job.Spec.Template.Spec.Volumes {
		if volume.Name == serviceCAVolumeName {
			hasServiceCAVolume = true
			break
		}
	}
	hasServiceCAMount := false
	if len(job.Spec.Template.Spec.Containers) > 0 {
		for _, mount := range job.Spec.Template.Spec.Containers[0].VolumeMounts {
			if mount.Name == serviceCAVolumeName {
				hasServiceCAMount = true
				break
			}
		}
	}
	logger.Info(
		"kubernetes job service-ca mount",
		"job_id", evaluation.Resource.ID,
		"benchmark_id", benchmarkID,
		"has_volume", hasServiceCAVolume,
		"has_mount", hasServiceCAMount,
		"mount_path", serviceCAMountPath,
	)

	logger.Info("kubernetes resource", "kind", "ConfigMap", "object", configMap)
	logger.Info("kubernetes resource", "kind", "NetworkPolicy", "object", networkPolicy)
	logger.Info("kubernetes resource", "kind", "Job", "object", job)

	// Create the ephemeral internalModelRef secret before the Job so the Pod can mount it.
	if jobConfig.modelInternalRefSecretName != "" {
		_, err := buildInternalModelRefSecret(ctx, jobConfig.namespace, jobConfig.modelInternalRefSecretName, secretInfo.data, jobConfig.sidecarBaseURL, jobLabels(jobConfig), r.helper)
		if err != nil {
			logger.Error("kubernetes internalModelRef secret create error", "namespace", jobConfig.namespace, "name", jobConfig.modelInternalRefSecretName, "error", err)
			return fmt.Errorf("job %s benchmark %s: internalModelRef secret: %w", evaluation.Resource.ID, benchmarkID, err)
		}
		logger.Info("kubernetes internalModelRef secret created", "namespace", jobConfig.namespace, "name", jobConfig.modelInternalRefSecretName)
	}

	// Cleanup must not use ctx: hard_delete cancels the create context, and
	// rollback of already-created objects still needs to reach the API.
	cleanupModelRefSecret := func() {
		if jobConfig.modelInternalRefSecretName == "" {
			return
		}
		cleanupCtx, cancel := orphanCleanupContext()
		defer cancel()
		if cleanupErr := r.helper.DeleteSecret(cleanupCtx, jobConfig.namespace, jobConfig.modelInternalRefSecretName, metav1.DeleteOptions{}); cleanupErr != nil && !apierrors.IsNotFound(cleanupErr) {
			logger.Error("failed to delete internalModelRef secret after error", "error", cleanupErr)
		}
	}

	createdConfigMap, err := r.helper.CreateConfigMap(ctx, configMap.Namespace, configMap.Name, configMap.Data, &CreateConfigMapOptions{
		Labels:      configMap.Labels,
		Annotations: configMap.Annotations,
	})
	if err != nil {
		logger.Error("kubernetes configmap create error", "namespace", configMap.Namespace, "name", configMap.Name, "error", err)
		cleanupModelRefSecret()
		return fmt.Errorf("job %s benchmark %s: %w", evaluation.Resource.ID, benchmarkID, err)
	}

	cleanupConfigMap := func() {
		cleanupCtx, cancel := orphanCleanupContext()
		defer cancel()
		if cleanupErr := r.helper.DeleteConfigMap(cleanupCtx, configMap.Namespace, configMap.Name); cleanupErr != nil && !apierrors.IsNotFound(cleanupErr) {
			logger.Error("failed to delete configmap after job creation error", "error", cleanupErr)
		}
	}

	configMapOwnerRef := metav1.OwnerReference{
		APIVersion: "v1",
		Kind:       "ConfigMap",
		Name:       createdConfigMap.Name,
		UID:        createdConfigMap.UID,
		Controller: boolPtr(true),
	}
	// The ConfigMap is created before the policy and temporarily owns it. If Job creation
	// fails, deleting the ConfigMap lets Kubernetes garbage-collect this policy without
	// granting the EvalHub ServiceAccount NetworkPolicy delete permission.
	networkPolicy.OwnerReferences = []metav1.OwnerReference{configMapOwnerRef}
	if _, err := r.helper.CreateNetworkPolicy(ctx, networkPolicy); err != nil {
		logger.Error("kubernetes network policy create error", "namespace", networkPolicy.Namespace, "name", networkPolicy.Name, "error", err)
		cleanupModelRefSecret()
		cleanupConfigMap()
		return fmt.Errorf("job %s benchmark %s: create network policy: %w", evaluation.Resource.ID, benchmarkID, err)
	}

	createdJob, err := r.helper.CreateJob(ctx, job)
	if err != nil {
		logger.Error("kubernetes job create error", "namespace", job.Namespace, "name", job.Name, "error", err)
		// Confirm/cleanup must not depend on ctx — it may already be cancelled by hard_delete.
		confirmCtx, cancelConfirm := orphanCleanupContext()
		defer cancelConfirm()
		existingJob, getErr := r.helper.GetJob(confirmCtx, job.Namespace, job.Name)
		switch {
		case apierrors.IsNotFound(getErr):
			cleanupModelRefSecret()
			cleanupConfigMap()
			return fmt.Errorf("job %s benchmark %s: %w", evaluation.Resource.ID, benchmarkID, err)
		case getErr == nil && jobMatchesExecution(existingJob, job):
			// The API may persist a create before the response is lost. Reuse only a Job
			// carrying this request's server-generated identity; keep its policy in place.
			createdJob = existingJob
			logger.Warn("job create response was ambiguous; found matching persisted job", "namespace", job.Namespace, "name", job.Name)
		case getErr != nil:
			// Uncertain: create failed and confirm GET also failed. The Job may still
			// exist — keep ConfigMap/secret (and ConfigMap-owned NetworkPolicy) until a
			// later Job delete (abort sweep / hard_delete). Do not delete supporting
			// resources while Job presence is unconfirmed.
			logger.Error(
				"unable to confirm job creation outcome; preserving supporting resources",
				"namespace", job.Namespace,
				"name", job.Name,
				"create_error", err,
				"confirm_error", getErr,
			)
			return errors.Join(
				fmt.Errorf("job %s benchmark %s: create job: %w", evaluation.Resource.ID, benchmarkID, err),
				fmt.Errorf("confirm job creation outcome: %w", getErr),
			)
		default:
			// A Job with this name exists but is not our execution — drop our supporting
			// resources so they do not linger as orphans.
			cleanupModelRefSecret()
			cleanupConfigMap()
			return errors.Join(
				fmt.Errorf("job %s benchmark %s: create job: %w", evaluation.Resource.ID, benchmarkID, err),
				fmt.Errorf("job %s exists but does not match the execution identity", job.Name),
			)
		}
	}
	ownerRef := metav1.OwnerReference{
		APIVersion: "batch/v1",
		Kind:       "Job",
		Name:       createdJob.Name,
		UID:        createdJob.UID,
		Controller: boolPtr(true),
	}
	if err := r.helper.SetNetworkPolicyOwner(ctx, networkPolicy.Namespace, networkPolicy.Name, ownerRef); err != nil {
		ownerErr := fmt.Errorf("job %s benchmark %s: set network policy owner: %w", evaluation.Resource.ID, benchmarkID, err)
		logger.Error("failed to set network policy owner reference; cleaning up job", "namespace", networkPolicy.Namespace, "name", networkPolicy.Name, "error", err)
		cleanupCtx, cancelCleanup := orphanCleanupContext()
		defer cancelCleanup()
		if delErr := r.helper.DeleteJob(cleanupCtx, createdJob.Namespace, createdJob.Name, jobForegroundDeleteOptions()); delErr != nil && !apierrors.IsNotFound(delErr) {
			return errors.Join(ownerErr, fmt.Errorf("delete job after network policy owner failure: %w", delErr))
		}
		// Foreground deletion is asynchronous. Retain the ConfigMap-owned policy until
		// Job deletion completes, so cleanup cannot remove protection from running Pods.
		if waitErr := wait.PollUntilContextTimeout(cleanupCtx, 100*time.Millisecond, orphanCleanupTimeout, true, func(pollCtx context.Context) (bool, error) {
			_, getErr := r.helper.GetJob(pollCtx, createdJob.Namespace, createdJob.Name)
			if apierrors.IsNotFound(getErr) {
				return true, nil
			}
			return false, getErr
		}); waitErr != nil {
			return errors.Join(ownerErr, fmt.Errorf("wait for job deletion after network policy owner failure: %w", waitErr))
		}
		cleanupModelRefSecret()
		cleanupConfigMap()
		return ownerErr
	}
	if err := r.helper.SetConfigMapOwner(ctx, configMap.Namespace, configMap.Name, ownerRef); err != nil {
		if apierrors.IsNotFound(err) {
			// ConfigMap disappeared before ownership could be set (e.g. creator rollback or
			// external delete). The Job can never mount it — delete the Job to avoid a stuck pod.
			logger.Warn("configmap missing mid-creation — cleaning up orphaned job",
				"namespace", createdJob.Namespace, "job", createdJob.Name, "configmap", configMap.Name)
			cleanupCtx, cancelCleanup := orphanCleanupContext()
			defer cancelCleanup()
			if delErr := r.helper.DeleteJob(cleanupCtx, createdJob.Namespace, createdJob.Name, jobForegroundDeleteOptions()); delErr != nil && !apierrors.IsNotFound(delErr) {
				logger.Error("failed to delete orphaned job", "namespace", createdJob.Namespace, "name", createdJob.Name, "error", delErr)
				return delErr
			}
			cleanupModelRefSecret()
			return nil
		}
		logger.Error("failed to set configmap owner reference", "namespace", configMap.Namespace, "name", configMap.Name, "error", err)
	}
	// Point the internalModelRef secret at the Job so Kubernetes GC deletes it when the Job is removed.
	// If SetSecretOwner fails, delete the secret immediately to avoid orphaning it.
	if jobConfig.modelInternalRefSecretName != "" {
		if err := r.helper.SetSecretOwner(ctx, jobConfig.namespace, jobConfig.modelInternalRefSecretName, ownerRef); err != nil {
			logger.Error("failed to set internalModelRef secret owner reference; cleaning up to avoid orphan", "namespace", jobConfig.namespace, "name", jobConfig.modelInternalRefSecretName, "error", err)
			cleanupModelRefSecret()
		}
	}
	return nil
}

func buildBenchmarkFailureStatus(benchmark *api.EvaluationBenchmarkConfig, benchmarkIndex int, runErr error) *api.StatusEvent {
	return &api.StatusEvent{
		BenchmarkStatusEvent: &api.BenchmarkStatusEvent{
			ProviderID:     benchmark.ProviderID,
			ID:             benchmark.ID,
			BenchmarkIndex: benchmarkIndex,
			Status:         api.StateFailed,
			ErrorMessage: api.WithMessageOrigin(&api.MessageInfo{
				Message:     runErr.Error(),
				MessageCode: constants.MessageCodeEvaluationJobFailed,
			}, api.MessageOriginServer),
		},
	}
}

func (r *K8sRuntime) Name() string {
	return "kubernetes"
}

// ListHardwareProfiles discovers enabled platform profiles. Queue-backed profiles
// are visible only when their LocalQueue exists in the request tenant namespace.
func (r *K8sRuntime) ListHardwareProfiles(ctx context.Context, namespace string) ([]api.HardwareProfileInfo, error) {
	if namespace == "" {
		return nil, fmt.Errorf("tenant namespace is required")
	}
	profileNamespace, err := hardwareProfilesNamespace()
	if err != nil {
		return nil, err
	}
	profiles, err := r.helper.ListHardwareProfiles(ctx, profileNamespace)
	if err != nil {
		return nil, err
	}
	var queues map[string]api.QueueInfo
	result := make([]api.HardwareProfileInfo, 0, len(profiles.Items))
	for _, profile := range profiles.Items {
		if isHardwareProfileDisabled(&profile) {
			continue
		}
		parsed, err := parseHardwareProfileResources(&profile)
		if err != nil {
			return nil, fmt.Errorf("parse hardware profile %q: %w", profile.GetName(), err)
		}
		var availability *api.QueueAvailability
		if parsed.schedulingType == hardwareProfileSchedulingQueue {
			if parsed.queueName == "" {
				continue
			}
			if queues == nil {
				listed, err := r.helper.ListLocalQueues(ctx, namespace)
				if err != nil {
					return nil, err
				}
				queues = make(map[string]api.QueueInfo, len(listed))
				for _, queue := range listed {
					queues[queue.Name] = queue
				}
			}
			queue, ok := queues[parsed.queueName]
			if !ok {
				continue
			}
			availability = &api.QueueAvailability{Status: queue.Status, Reason: queue.Reason, Message: queue.Message}
		}
		annotations := profile.GetAnnotations()
		result = append(result, api.HardwareProfileInfo{
			Name:              profile.GetName(),
			QueueName:         parsed.queueName,
			PriorityClassName: parsed.priorityClassName,
			SchedulingType:    api.HardwareProfileSchedulingType(strings.ToLower(parsed.schedulingType)),
			Identifiers:       parsed.identifiers,
			QueueAvailability: availability,
			DisplayName:       annotations["opendatahub.io/display-name"],
			Description:       annotations["opendatahub.io/description"],
		})
	}
	slices.SortFunc(result, func(a, b api.HardwareProfileInfo) int {
		return strings.Compare(a.Name, b.Name)
	})
	return result, nil
}

// ValidateHardwareProfiles ensures referenced HardwareProfiles exist, are enabled,
// and can be parsed. Called from the create handler before the job is persisted.
func (r *K8sRuntime) ValidateHardwareProfiles(benchmarks []api.EvaluationBenchmarkConfig) error {
	ctx := r.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	logger := r.logger
	for _, benchmark := range benchmarks {
		if benchmark.HardwareConfig == nil {
			continue
		}
		profileName := strings.TrimSpace(benchmark.HardwareConfig.HardwareProfileName)
		if profileName == "" {
			continue
		}
		profileNamespace, err := hardwareProfilesNamespace()
		if err != nil {
			if logger != nil {
				logger.Error(
					"hardware profiles namespace is not configured",
					"error", err,
					"env", hardwareProfilesNamespaceEnv,
					"profile", profileName,
				)
			}
			return serviceerrors.NewServiceError(messages.HardwareProfileFetchFailed, "Name", profileName)
		}
		profileCR, err := r.helper.GetHardwareProfile(ctx, profileNamespace, profileName)
		if err != nil {
			if apierrors.IsNotFound(err) {
				return serviceerrors.NewServiceError(messages.HardwareProfileNotFound, "Name", profileName)
			}
			if logger != nil {
				logger.Error(
					"failed to fetch hardware profile",
					"error", err,
					"namespace", profileNamespace,
					"profile", profileName,
				)
			}
			return serviceerrors.NewServiceError(messages.HardwareProfileFetchFailed, "Name", profileName)
		}
		if isHardwareProfileDisabled(profileCR) {
			return serviceerrors.NewServiceError(messages.HardwareProfileDisabled, "Name", profileName)
		}
		if _, err := parseHardwareProfileResources(profileCR); err != nil {
			return serviceerrors.NewServiceError(messages.HardwareProfileInvalid, "Name", profileName, "Error", err.Error())
		}
	}
	return nil
}

// rewriteModelURLForSidecar returns a URL with the scheme and host of sidecarBaseURL
// but the path (and any query/fragment) of modelURL. This lets the adapter call the
// sidecar at localhost while preserving the full path prefix from the user's model URL
// (e.g. /llm/llama-3-1-8b-instruct/v1 for a KServe Gateway API ingress route).
// The sidecar Rewrite function forwards the incoming path verbatim to the real model
// host (it only swaps scheme+host), so the full path is preserved end-to-end.
func rewriteModelURLForSidecar(sidecarBaseURL, modelURL string) (string, error) {
	sidecar, err := url.Parse(strings.TrimSuffix(sidecarBaseURL, "/"))
	if err != nil {
		return "", fmt.Errorf("invalid sidecar base URL %q: %w", sidecarBaseURL, err)
	}
	if sidecar.Host == "" {
		return "", fmt.Errorf("invalid sidecar base URL %q: missing host", sidecarBaseURL)
	}
	model, err := url.Parse(strings.TrimSuffix(modelURL, "/"))
	if err != nil {
		return "", fmt.Errorf("invalid model URL %q: %w", modelURL, err)
	}
	out := &url.URL{
		Scheme:   sidecar.Scheme,
		Host:     sidecar.Host,
		Path:     model.Path,
		RawQuery: model.RawQuery,
		Fragment: model.Fragment,
	}
	return out.String(), nil
}

// resolveMLFlowCABundleConfigMap enables mounting {instance}-mlflow-ca-bundle only when
// that ConfigMap already exists in the job namespace with a non-empty ca-bundle.crt entry.
// The job-pod mount path is independent of the EvalHub API's MLFLOW_CA_CERT_PATH.
func (r *K8sRuntime) resolveMLFlowCABundleConfigMap(ctx context.Context, cfg *jobConfig, logger *slog.Logger) string {
	if cfg == nil || cfg.evalHubInstanceName == "" || cfg.mlflowTrackingURI == "" {
		return ""
	}
	cmName := mlflowCABundleConfigMapName(cfg.evalHubInstanceName)
	cm, err := r.helper.GetConfigMap(ctx, cfg.namespace, cmName)
	if err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info(
				"MLflow CA bundle ConfigMap not found; falling back to service CA / configured CA path",
				"configmap", cmName,
				"namespace", cfg.namespace,
			)
			return ""
		}
		logger.Warn(
			"failed to check MLflow CA bundle ConfigMap; falling back to service CA / configured CA path",
			"configmap", cmName,
			"namespace", cfg.namespace,
			"error", err,
		)
		return ""
	}
	if strings.TrimSpace(cm.Data[mlflowCABundleFile]) == "" {
		logger.Info(
			"MLflow CA bundle ConfigMap missing or empty ca-bundle.crt; falling back to service CA / configured CA path",
			"configmap", cmName,
			"namespace", cfg.namespace,
			"key", mlflowCABundleFile,
		)
		return ""
	}
	return cmName
}
