package k8s

// Runtime entrypoints for Kubernetes job creation.
import (
	"context"
	"fmt"
	"log/slog"

	"github.com/eval-hub/eval-hub/internal/abstractions"
	"github.com/eval-hub/eval-hub/pkg/api"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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

	provider, benchmarkID, err := resolveProviderFromEvaluation(r.providers, evaluation)
	if err != nil {
		return err
	}
	jobConfig, err := buildJobConfig(evaluation, provider, benchmarkID)
	if err != nil {
		return err
	}
	configMap := buildConfigMap(jobConfig)
	job, err := buildJob(jobConfig)
	if err != nil {
		return err
	}

	ctx := context.Background()
	_, err = r.helper.CreateConfigMap(ctx, configMap.Namespace, configMap.Name, configMap.Data, &CreateConfigMapOptions{
		Labels:      configMap.Labels,
		Annotations: configMap.Annotations,
	})
	if err != nil {
		return err
	}

	_, err = r.helper.CreateJob(ctx, job)
	if err != nil {
		cleanupErr := r.helper.DeleteConfigMap(ctx, configMap.Namespace, configMap.Name)
		if cleanupErr != nil && !apierrors.IsNotFound(cleanupErr) {
			if r.logger != nil {
				r.logger.Error("failed to delete configmap after job creation error", "error", cleanupErr)
			}
		}
		return err
	}
	return nil
}

func resolveProviderFromEvaluation(providers map[string]api.ProviderResource, evaluation *api.EvaluationJobResource) (*api.ProviderResource, string, error) {
	if len(providers) == 0 {
		return nil, "", fmt.Errorf("no provider configs loaded")
	}
	if len(evaluation.Benchmarks) == 0 {
		return nil, "", fmt.Errorf("evaluation contains no benchmarks")
	}
	if len(evaluation.Benchmarks) > 1 {
		return nil, "", fmt.Errorf("multi-benchmark evaluations are not supported (count: %d)", len(evaluation.Benchmarks))
	}

	// TODO for now, picked the first benchmark from the list
	benchmarkID := evaluation.Benchmarks[0].ID
	if benchmarkID == "" {
		return nil, "", fmt.Errorf("evaluation benchmark id is empty")
	}
	for _, provider := range providers {
		for _, providerBenchmark := range provider.Benchmarks {
			if providerBenchmark.BenchmarkId == benchmarkID {
				providerCopy := provider
				return &providerCopy, benchmarkID, nil
			}
		}
	}
	return nil, "", fmt.Errorf("no provider found for benchmark %q", benchmarkID)
}

func (r *K8sRuntime) Name() string {
	return "kubernetes"
}
