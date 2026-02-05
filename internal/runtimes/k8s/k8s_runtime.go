package k8s

// Runtime entrypoints for Kubernetes job creation.
import (
	"context"
	"fmt"
	"log/slog"

	"github.com/eval-hub/eval-hub/internal/abstractions"
	"github.com/eval-hub/eval-hub/internal/constants"
	"github.com/eval-hub/eval-hub/pkg/api"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const maxBenchmarkWorkers = 5

type K8sRuntime struct {
	logger    *slog.Logger
	helper    *KubernetesHelper
	providers map[string]api.ProviderResource
	ctx       context.Context
}

// NewK8sRuntime creates a Kubernetes runtime.
func NewK8sRuntime(logger *slog.Logger, providerConfigs map[string]api.ProviderResource) (abstractions.Runtime, error) {
	helper, err := NewKubernetesHelper()
	if err != nil {
		return nil, err
	}
	return &K8sRuntime{logger: logger, helper: helper, providers: providerConfigs}, nil
}

func (r *K8sRuntime) WithLogger(logger *slog.Logger) abstractions.Runtime {
	return &K8sRuntime{
		logger:    logger,
		helper:    r.helper,
		providers: r.providers,
		ctx:       r.ctx,
	}
}

func (r *K8sRuntime) WithContext(ctx context.Context) abstractions.Runtime {
	return &K8sRuntime{
		logger:    r.logger,
		helper:    r.helper,
		providers: r.providers,
		ctx:       ctx,
	}
}

func (r *K8sRuntime) RunEvaluationJob(evaluation *api.EvaluationJobResource, storage *abstractions.Storage) error {
	benchmarks := make(chan api.BenchmarkConfig, len(evaluation.Benchmarks))
	for _, bench := range evaluation.Benchmarks {
		benchmarks <- bench
	}
	close(benchmarks)

	workerCount := maxBenchmarkWorkers
	if len(evaluation.Benchmarks) < workerCount {
		workerCount = len(evaluation.Benchmarks)
	}

	for i := 0; i < workerCount; i++ {
		go func() {
			for bench := range benchmarks {
				select {
				case <-r.ctx.Done():
					r.logger.Warn(
						"benchmark processing canceled",
						"job_id", evaluation.Resource.ID,
						"benchmark_id", bench.ID,
					)
					return
				default:
				}
				if err := r.createBenchmarkResources(r.ctx, r.logger, evaluation, &bench); err != nil {
					r.logger.Error(
						"kubernetes job creation failed",
						"error", err,
						"job_id", evaluation.Resource.ID,
						"benchmark_id", bench.ID,
					)

					if storage != nil && *storage != nil {
						runStatus := buildBenchmarkFailureStatus(&bench, err)
						if updateErr := (*storage).UpdateEvaluationJob(evaluation.Resource.ID, runStatus); updateErr != nil {
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
	}

	return nil
}

func (r *K8sRuntime) createBenchmarkResources(ctx context.Context, logger *slog.Logger, evaluation *api.EvaluationJobResource, benchmark *api.BenchmarkConfig) error {
	benchmarkID := benchmark.ID
	// Provider/benchmark validation should be handled during creation.
	provider := r.providers[benchmark.ProviderID]
	jobConfig, err := buildJobConfig(evaluation, &provider, benchmarkID)
	if err != nil {
		logger.Error("kubernetes job config error", "benchmark_id", benchmarkID, "error", err)
		return fmt.Errorf("job %s benchmark %s: %w", evaluation.Resource.ID, benchmarkID, err)
	}
	configMap := buildConfigMap(jobConfig)
	job, err := buildJob(jobConfig)
	if err != nil {
		logger.Error("kubernetes job build error", "benchmark_id", benchmarkID, "error", err)
		return fmt.Errorf("job %s benchmark %s: %w", evaluation.Resource.ID, benchmarkID, err)
	}

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
		logger.Error("failed to set configmap owner reference", "namespace", configMap.Namespace, "name", configMap.Name, "error", err)
	}
	return nil
}

func buildBenchmarkFailureStatus(benchmark *api.BenchmarkConfig, runErr error) *api.RunStatusInternal {
	return &api.RunStatusInternal{
		StatusEvent: api.RunStatusEvent{
			ProviderID:   benchmark.ProviderID,
			BenchmarkID:  benchmark.ID,
			Status:       api.StateFailed,
			ErrorMessage: &api.MessageInfo{Message: runErr.Error(), MessageCode: constants.MESSAGE_CODE_EVALUATION_JOB_FAILED},
		},
	}
}

func (r *K8sRuntime) Name() string {
	return "kubernetes"
}
