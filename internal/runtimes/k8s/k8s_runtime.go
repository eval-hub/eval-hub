package k8s

// Runtime entrypoints for Kubernetes job creation.
import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/eval-hub/eval-hub/internal/abstractions"
	"github.com/eval-hub/eval-hub/internal/constants"
	"github.com/eval-hub/eval-hub/internal/executioncontext"
	"github.com/eval-hub/eval-hub/pkg/api"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type K8sRuntime struct {
	logger    *slog.Logger
	helper    *KubernetesHelper
	providers map[string]api.ProviderResource
}

// NewK8sRuntime creates a Kubernetes runtime.
func NewK8sRuntime(logger *slog.Logger, providerConfigs map[string]api.ProviderResource) (abstractions.Runtime, error) {
	helper, err := NewKubernetesHelper()
	if err != nil {
		return nil, err
	}
	return &K8sRuntime{logger: logger, helper: helper, providers: providerConfigs}, nil
}

func (r *K8sRuntime) RunEvaluationJob(evaluation *api.EvaluationJobResource, storage *abstractions.Storage) error {
	_ = storage
	if evaluation == nil {
		return fmt.Errorf("evaluation is required")
	}

	ctx := context.Background()
	errCh := make(chan error, len(evaluation.Benchmarks))
	var wg sync.WaitGroup

	for i := range evaluation.Benchmarks {
		benchmark := evaluation.Benchmarks[i]
		wg.Go(func() {
			if err := r.createBenchmarkResources(ctx, evaluation, &benchmark); err != nil {
				errCh <- err
			}
		})
	}

	wg.Wait()
	close(errCh)
	var combinedErr error
	var errMessages []string
	for err := range errCh {
		if err != nil {
			errMessages = append(errMessages, err.Error())
			if combinedErr == nil {
				combinedErr = err
			}
		}
	}
	if combinedErr != nil {
		if len(errMessages) > 1 {
			combinedErr = fmt.Errorf("%s", strings.Join(errMessages, "; "))
		}
		r.logger.Error("kubernetes job creation failed", "error", combinedErr, "job_id", evaluation.Resource.ID)
		r.persistJobFailure(storage, evaluation, combinedErr)
		return combinedErr
	}
	return nil
}

func (r *K8sRuntime) createBenchmarkResources(ctx context.Context, evaluation *api.EvaluationJobResource, benchmark *api.BenchmarkConfig) error {
	benchmarkID := benchmark.ID
	// Provider/benchmark validation should be handled during creation.
	provider := r.providers[benchmark.ProviderID]
	jobConfig, err := buildJobConfig(evaluation, &provider, benchmarkID)
	if err != nil {
		r.logger.Error("kubernetes job config error", "benchmark_id", benchmarkID, "error", err)
		return fmt.Errorf("job %s benchmark %s: %w", evaluation.Resource.ID, benchmarkID, err)
	}
	configMap := buildConfigMap(jobConfig)
	job, err := buildJob(jobConfig)
	if err != nil {
		r.logger.Error("kubernetes job build error", "benchmark_id", benchmarkID, "error", err)
		return fmt.Errorf("job %s benchmark %s: %w", evaluation.Resource.ID, benchmarkID, err)
	}

	r.logger.Info("kubernetes resource", "kind", "ConfigMap", "object", configMap)
	r.logger.Info("kubernetes resource", "kind", "Job", "object", job)

	_, err = r.helper.CreateConfigMap(ctx, configMap.Namespace, configMap.Name, configMap.Data, &CreateConfigMapOptions{
		Labels:      configMap.Labels,
		Annotations: configMap.Annotations,
	})
	if err != nil {
		r.logger.Error("kubernetes configmap create error", "namespace", configMap.Namespace, "name", configMap.Name, "error", err)
		return fmt.Errorf("job %s benchmark %s: %w", evaluation.Resource.ID, benchmarkID, err)
	}

	createdJob, err := r.helper.CreateJob(ctx, job)
	if err != nil {
		r.logger.Error("kubernetes job create error", "namespace", job.Namespace, "name", job.Name, "error", err)
		cleanupErr := r.helper.DeleteConfigMap(ctx, configMap.Namespace, configMap.Name)
		if cleanupErr != nil && !apierrors.IsNotFound(cleanupErr) {
			if r.logger != nil {
				r.logger.Error("failed to delete configmap after job creation error", "error", cleanupErr)
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
		r.logger.Error("failed to set configmap owner reference", "namespace", configMap.Namespace, "name", configMap.Name, "error", err)
	}
	return nil
}

func (r *K8sRuntime) persistJobFailure(storage *abstractions.Storage, evaluation *api.EvaluationJobResource, runErr error) {
	if storage == nil || *storage == nil || evaluation == nil {
		return
	}
	status := &api.EvaluationJobStatus{
		EvaluationJobState: api.EvaluationJobState{
			State: api.StateFailed,
			Message: &api.MessageInfo{
				Message:     runErr.Error(),
				MessageCode: constants.MESSAGE_CODE_EVALUATION_JOB_FAILED,
			},
		},
	}
	ctx := &executioncontext.ExecutionContext{
		Ctx:       context.Background(),
		RequestID: "",
		Logger:    r.logger,
		StartedAt: time.Now(),
	}
	if err := (*storage).UpdateEvaluationJobStatus(ctx, evaluation.Resource.ID, status); err != nil {
		r.logger.Error("failed to update evaluation status", "error", err, "job_id", evaluation.Resource.ID)
	}
}

func (r *K8sRuntime) Name() string {
	return "kubernetes"
}
