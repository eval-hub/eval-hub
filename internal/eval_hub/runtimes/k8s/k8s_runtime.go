package k8s

// Runtime entrypoints for Kubernetes job creation.
import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
	"github.com/eval-hub/eval-hub/internal/eval_hub/constants"
	"github.com/eval-hub/eval-hub/internal/eval_hub/messages"
	"github.com/eval-hub/eval-hub/internal/eval_hub/serviceerrors"
	"github.com/eval-hub/eval-hub/pkg/api"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type K8sRuntime struct {
	logger        *slog.Logger
	serviceConfig *config.Config
	helper        *KubernetesHelper
	ctx           context.Context
}

// NewK8sRuntime creates a Kubernetes runtime.
func NewK8sRuntime(logger *slog.Logger, serviceConfig *config.Config) (abstractions.Runtime, error) {
	helper, err := NewKubernetesHelper()
	if err != nil {
		return nil, err
	}
	return &K8sRuntime{logger: logger, serviceConfig: serviceConfig, helper: helper}, nil
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

func (r *K8sRuntime) RunEvaluationJob(
	evaluation *api.EvaluationJobResource,
	benchmarks []api.EvaluationBenchmarkConfig,
	storage abstractions.RuntimeStorage,
) error {
	if len(benchmarks) == 0 {
		return serviceerrors.NewServiceError(messages.EvaluationJobEmpty, "EvaluationJobID", evaluation.Resource.ID)
	}

	go func() {
		for idx, bench := range benchmarks {
			benchCtx := context.Background()
			if err := r.createBenchmarkResources(benchCtx, r.logger, evaluation, &bench, idx, storage); err != nil {
				r.logger.Error(
					"kubernetes job creation failed",
					"error", err,
					"job_id", evaluation.Resource.ID,
					"benchmark_id", bench.ID,
				)

				if storage != nil {
					runStatus := buildBenchmarkFailureStatus(&bench, idx, err)
					if updateErr := storage.UpdateEvaluationJob(evaluation.Resource.ID, runStatus); updateErr != nil {
						r.logger.Error(
							"failed to update benchmark status",
							"error", updateErr,
							"job_id", evaluation.Resource.ID,
							"benchmark_id", bench.ID,
						)
					}
				}
			}
		}
	}()
	return nil
}

func (r *K8sRuntime) DeleteEvaluationJobResources(evaluation *api.EvaluationJobResource) error {
	namespace := resolveNamespace(string(evaluation.Resource.Tenant))
	propagationPolicy := metav1.DeletePropagationBackground
	deleteOptions := metav1.DeleteOptions{PropagationPolicy: &propagationPolicy}

	r.logger.Info(
		"deleting evaluation runtime resources",
		"job_id", evaluation.Resource.ID,
		"benchmark_count", len(evaluation.Benchmarks),
		"namespace", namespace,
	)

	labelSelector := fmt.Sprintf("%s=%s", labelJobIDKey, sanitizeLabelValue(evaluation.Resource.ID))
	jobs, err := r.helper.ListJobs(r.ctx, namespace, labelSelector)
	if err != nil {
		return err
	}
	configMaps, err := r.helper.ListConfigMaps(r.ctx, namespace, labelSelector)
	if err != nil {
		return err
	}

	var deleteErr error
	for _, job := range jobs {
		r.logger.Info(
			"deleting evaluation runtime job",
			"job_id", evaluation.Resource.ID,
			"job_name", job.Name,
			"namespace", namespace,
		)
		if err := r.helper.DeleteJob(r.ctx, namespace, job.Name, deleteOptions); err != nil && !apierrors.IsNotFound(err) {
			deleteErr = errors.Join(deleteErr, err)
		}
	}
	// OwnerReferences should GC ConfigMaps when Jobs are deleted, but we delete explicitly
	// to avoid orphans if the owner ref was never set or the job delete is delayed.
	for _, configMap := range configMaps {
		r.logger.Info(
			"deleting evaluation runtime configmap",
			"job_id", evaluation.Resource.ID,
			"configmap_name", configMap.Name,
			"namespace", namespace,
		)
		if err := r.helper.DeleteConfigMap(r.ctx, namespace, configMap.Name); err != nil && !apierrors.IsNotFound(err) {
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
	provider, err := storage.GetProvider(benchmark.ProviderID)
	if err != nil {
		return err
	}
	var hardwareProfile *hardwareProfileResources
	if benchmark.HardwareConfig != nil {
		ref := benchmark.HardwareConfig.HardwareProfileRef
		profileName := strings.TrimSpace(ref.Name)
		if profileName != "" {
			profileNamespace := resolveHardwareProfileNamespace(ref.Namespace, string(evaluation.Resource.Tenant))
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
	logger.Info(
		"kubernetes job config",
		"job_id", evaluation.Resource.ID,
		"benchmark_id", benchmarkID,
		"service_account", jobConfig.serviceAccountName,
		"service_ca_configmap", jobConfig.serviceCAConfigMap,
		"eval_hub_url", jobConfig.evalHubURL,
	)
	configMap, err := buildConfigMap(jobConfig)
	if err != nil {
		logger.Error("kubernetes configmap build error", "benchmark_id", benchmarkID, "error", err)
		return fmt.Errorf("job %s benchmark %s: %w", evaluation.Resource.ID, benchmarkID, err)
	}
	job, err := buildJob(jobConfig)
	if err != nil {
		logger.Error("kubernetes job build error", "benchmark_id", benchmarkID, "error", err)
		return fmt.Errorf("job %s benchmark %s: %w", evaluation.Resource.ID, benchmarkID, err)
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
	logger.Info("kubernetes resource", "kind", "Job", "object", job)

	_, err = r.helper.CreateConfigMap(ctx, configMap.Namespace, configMap.Name, configMap.Data, &CreateConfigMapOptions{
		Labels:      configMap.Labels,
		Annotations: configMap.Annotations,
	})
	if err != nil {
		logger.Error("kubernetes configmap create error", "namespace", configMap.Namespace, "name", configMap.Name, "error", err)
		return fmt.Errorf("job %s benchmark %s: %w", evaluation.Resource.ID, benchmarkID, err)
	}

	createdJob, err := r.helper.CreateJob(ctx, job)
	if err != nil {
		logger.Error("kubernetes job create error", "namespace", job.Namespace, "name", job.Name, "error", err)
		cleanupErr := r.helper.DeleteConfigMap(ctx, configMap.Namespace, configMap.Name)
		if cleanupErr != nil && !apierrors.IsNotFound(cleanupErr) {
			if logger != nil {
				logger.Error("failed to delete configmap after job creation error", "error", cleanupErr)
			}
		}
		return fmt.Errorf("job %s benchmark %s: %w", evaluation.Resource.ID, benchmarkID, err)
	}
	ownerRef := metav1.OwnerReference{
		APIVersion: "batch/v1",
		Kind:       "Job",
		Name:       createdJob.Name,
		UID:        createdJob.UID,
		Controller: boolPtr(true),
	}
	if err := r.helper.SetConfigMapOwner(ctx, configMap.Namespace, configMap.Name, ownerRef); err != nil {
		if apierrors.IsNotFound(err) {
			// Race: hard_delete arrived during creation and removed the ConfigMap before the
			// owner reference could be set. The K8s Job was created but can never mount the
			// missing ConfigMap — delete it now to prevent an orphaned, permanently-stuck job.
			logger.Warn("configmap deleted mid-creation (race with hard_delete) — cleaning up orphaned job",
				"namespace", createdJob.Namespace, "job", createdJob.Name, "configmap", configMap.Name)
			if delErr := r.helper.DeleteJob(ctx, createdJob.Namespace, createdJob.Name, metav1.DeleteOptions{}); delErr != nil && !apierrors.IsNotFound(delErr) {
				logger.Error("failed to delete orphaned job", "namespace", createdJob.Namespace, "name", createdJob.Name, "error", delErr)
				return delErr
			}
			return nil
		}
		logger.Error("failed to set configmap owner reference", "namespace", configMap.Namespace, "name", configMap.Name, "error", err)
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
			ErrorMessage:   &api.MessageInfo{Message: runErr.Error(), MessageCode: constants.MESSAGE_CODE_EVALUATION_JOB_FAILED},
		},
	}
}

func (r *K8sRuntime) Name() string {
	return "kubernetes"
}
