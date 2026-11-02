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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

type K8sRuntime struct {
	logger        *slog.Logger
	serviceConfig *config.Config
	helper        *KubernetesHelper
	ctx           context.Context
	// shutdownCtx is cancelled by Close so in-flight create loops stop waiting
	// on unknown abort checks during service shutdown.
	shutdownCtx    context.Context
	shutdownCancel context.CancelFunc
	// Test hooks (nil in production). Used to pause/observe create vs delete races
	// without blocking inside fake-clientset reactors (those hold the clientset lock).
	beforeCreateBenchmarkResources func()
	// beforeCreateJob runs after ConfigMap + NetworkPolicy exist and before CreateJob,
	// covering the window where the ConfigMap still owns the NetworkPolicy.
	beforeCreateJob               func()
	afterCreateBenchmarkResources func(err error)
	beforeSweepAfterAbort         func()
	// afterCreateLoop runs when the RunEvaluationJob background goroutine exits
	// (all benchmarks processed, or aborted early).
	afterCreateLoop func()
}

// orphanCleanupTimeout bounds K8s deletes that must succeed even when the
// caller's context (HTTP DELETE or cancelled create) is already done.
const orphanCleanupTimeout = 30 * time.Second

// createAbortBackoff* is the single capped exponential backoff used while the
// create goroutine stays paused until evaluation state is known or shutdown
// cancels. createAbortReadTimeout bounds each GetEvaluationJob. Tunable in tests.
var (
	createAbortBackoffInitial = 100 * time.Millisecond
	createAbortBackoffMax     = 5 * time.Second
	createAbortReadTimeout    = 5 * time.Second
)

func orphanCleanupContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), orphanCleanupTimeout)
}

// NewK8sRuntime creates a Kubernetes runtime.
func NewK8sRuntime(logger *slog.Logger, serviceConfig *config.Config) (abstractions.Runtime, error) {
	helper, err := NewKubernetesHelper()
	if err != nil {
		return nil, err
	}
	shutdownCtx, shutdownCancel := context.WithCancel(context.Background())
	return &K8sRuntime{
		logger:         logger,
		serviceConfig:  serviceConfig,
		helper:         helper,
		shutdownCtx:    shutdownCtx,
		shutdownCancel: shutdownCancel,
	}, nil
}

func (r *K8sRuntime) WithLogger(logger *slog.Logger) abstractions.Runtime {
	return &K8sRuntime{
		logger:         logger,
		serviceConfig:  r.serviceConfig,
		helper:         r.helper,
		ctx:            r.ctx,
		shutdownCtx:    r.shutdownCtx,
		shutdownCancel: r.shutdownCancel,
	}
}

func (r *K8sRuntime) WithContext(ctx context.Context) abstractions.Runtime {
	return &K8sRuntime{
		logger:         r.logger,
		serviceConfig:  r.serviceConfig,
		helper:         r.helper,
		ctx:            ctx,
		shutdownCtx:    r.shutdownCtx,
		shutdownCancel: r.shutdownCancel,
	}
}

// Close cancels in-flight abort waits and stops the Kubernetes EventBroadcaster
// background goroutines started by NewKubernetesHelper.
// WithLogger/WithContext copies share the same helper and shutdown cancel, so
// Close must be called exactly once on the root runtime returned by NewK8sRuntime.
func (r *K8sRuntime) Close() error {
	if r.shutdownCancel != nil {
		r.shutdownCancel()
	}
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
		if r.afterCreateLoop != nil {
			defer r.afterCreateLoop()
		}
		for idx, bench := range benchmarks {
			// Read evaluation state (retrying storage errors with capped backoff):
			// deleted/cancelled → clean up and stop; active → create this benchmark.
			if r.handleCreateAbortDecision(evaluation, storage, jobID, bench.ID, idx, nil, "pre-create") {
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
			// Read evaluation state again: deleted/cancelled → clean up late Job and
			// stop; active → continue (and record confirmed create failures below).
			if r.handleCreateAbortDecision(evaluation, storage, jobID, bench.ID, idx, err, "post-create") {
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

// handleCreateAbortDecision pauses the create goroutine until evaluation state is
// known (or shutdown). Returns true when the loop must stop.
func (r *K8sRuntime) handleCreateAbortDecision(
	evaluation *api.EvaluationJobResource,
	storage abstractions.RuntimeStorage,
	jobID, benchmarkID string,
	benchmarkIndex int,
	createErr error,
	phase string,
) bool {
	status, reason := r.awaitCreateAbortStatus(storage, jobID, benchmarkID)
	switch status {
	case createAbortActive:
		return false
	case createAbortAborted:
		r.logger.Info(
			"kubernetes job creation aborted; evaluation deleted or cancelled; sweeping leftovers",
			"job_id", jobID,
			"benchmark_id", benchmarkID,
			"benchmark_index", benchmarkIndex,
			"reason", reason,
			"phase", phase,
			"error", createErr,
		)
		r.sweepEvaluationResourcesAfterAbort(evaluation, jobID, benchmarkID)
		return true
	default:
		r.logger.Error(
			"storage abort check interrupted by shutdown while still unknown; stopping creates",
			"job_id", jobID,
			"benchmark_id", benchmarkID,
			"benchmark_index", benchmarkIndex,
			"phase", phase,
			"error", createErr,
		)
		return true
	}
}

func (r *K8sRuntime) abortWaitContext() context.Context {
	if r.shutdownCtx != nil {
		return r.shutdownCtx
	}
	return context.Background()
}

// runtimeStorageWithContext is implemented by handlers.runtimeStorage so each
// abort-check read can use a shutdown-linked timeout context.
type runtimeStorageWithContext interface {
	WithContext(ctx context.Context) abstractions.RuntimeStorage
}

// awaitCreateAbortStatus keeps creation paused and retries GetEvaluationJob with
// capped exponential backoff until state is definitive or the runtime shuts down.
func (r *K8sRuntime) awaitCreateAbortStatus(
	storage abstractions.RuntimeStorage,
	jobID, benchmarkID string,
) (createAbortStatus, string) {
	ctx := r.abortWaitContext()
	interval := createAbortBackoffInitial
	for attempt := 1; ; attempt++ {
		status, reason := r.checkCreateAbort(ctx, storage, jobID)
		switch status {
		case createAbortAborted:
			if attempt > 1 {
				r.logger.Info(
					"abort check confirmed delete/cancel after retries",
					"job_id", jobID,
					"benchmark_id", benchmarkID,
					"reason", reason,
					"attempt", attempt,
				)
			}
			return status, reason
		case createAbortActive:
			if attempt > 1 {
				r.logger.Info(
					"abort check confirmed evaluation still active; resuming creates",
					"job_id", jobID,
					"benchmark_id", benchmarkID,
					"attempt", attempt,
				)
			}
			return status, reason
		default:
			r.logger.Warn(
				"abort check storage state unknown; backing off",
				"job_id", jobID,
				"benchmark_id", benchmarkID,
				"attempt", attempt,
				"backoff", interval.String(),
			)
			timer := time.NewTimer(interval)
			select {
			case <-ctx.Done():
				timer.Stop()
				return createAbortUnknown, ""
			case <-timer.C:
			}
			if next := interval * 2; next <= createAbortBackoffMax {
				interval = next
			} else {
				interval = createAbortBackoffMax
			}
		}
	}
}

// checkCreateAbort performs one storage read. Transient errors and cancelled /
// timed-out reads are unknown so the caller retries. Each read uses a timeout
// derived from the runtime shutdown context when storage supports WithContext.
func (r *K8sRuntime) checkCreateAbort(
	parent context.Context,
	storage abstractions.RuntimeStorage,
	jobID string,
) (createAbortStatus, string) {
	if storage == nil {
		return createAbortActive, ""
	}
	readCtx, cancel := context.WithTimeout(parent, createAbortReadTimeout)
	defer cancel()

	scoped := storage
	if withCtx, ok := storage.(runtimeStorageWithContext); ok {
		scoped = withCtx.WithContext(readCtx)
	} else if err := readCtx.Err(); err != nil {
		return createAbortUnknown, ""
	}

	job, err := scoped.GetEvaluationJob(jobID)
	if err != nil {
		var serviceErr *serviceerrors.ServiceError
		if errors.As(err, &serviceErr) && serviceErr.MessageCode() == messages.ResourceNotFound {
			return createAbortAborted, "evaluation job deleted"
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return createAbortUnknown, ""
		}
		return createAbortUnknown, ""
	}
	if job == nil {
		return createAbortAborted, "evaluation job deleted"
	}
	if job.Status != nil && job.Status.State == api.OverallStateCancelled {
		return createAbortAborted, "evaluation job cancelled"
	}
	return createAbortActive, ""
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

// createAbortStatus is the tri-state result of a create-loop storage check.
// Unknown must not be treated as active: that leaves orphan Jobs after delete.
type createAbortStatus int

const (
	createAbortActive createAbortStatus = iota
	createAbortAborted
	createAbortUnknown
)

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
	// Delete Jobs (and ref secrets). Job-owned ConfigMaps/NetworkPolicies are left to
	// Kubernetes GC. Do not delete "unowned" ConfigMaps here: an empty Job list cannot
	// prove another replica has stopped creating, and deleting a ConfigMap that still
	// owns a NetworkPolicy would strip ingress protection from a late-created Job.
	// Creator rollback covers confirmed-failed creates; uncertain leftovers need
	// separate coordination (out of scope for the delete path).
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

	if r.beforeCreateJob != nil {
		r.beforeCreateJob()
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
