// Package evaluations provides the conventional evaluation workload.
package evaluations

import (
	"fmt"

	"github.com/eval-hub/eval-hub/internal/eval_hub/constants"
	"github.com/eval-hub/eval-hub/internal/eval_hub/messages"
	"github.com/eval-hub/eval-hub/internal/eval_hub/serviceerrors"
	"github.com/eval-hub/eval-hub/internal/eval_hub/workloads"
	"github.com/eval-hub/eval-hub/pkg/api"
)

// Workload implements conventional evaluation identification and API conversion.
type Workload struct{}

var _ workloads.Workload = Workload{}

func init() {
	workloads.Register(Workload{})
}

func (Workload) Type() workloads.Type                   { return workloads.Evaluation }
func (Workload) ProviderID() string                     { return "" }
func (Workload) BenchmarkID() string                    { return "" }
func (Workload) Internal() bool                         { return false }
func (Workload) RuntimeProvider() *api.ProviderResource { return nil }
func (Workload) NewRequest() any                        { return &api.EvaluationJobConfig{} }
func (Workload) MatchesJob(job *api.EvaluationJobConfig) bool {
	return job != nil && workloads.TypeForJob(job) == workloads.Evaluation
}

func (Workload) GetOptions() workloads.GetOptions {
	return workloads.GetOptions{
		PathParameter:    constants.PathParameterJobID,
		ResourceName:     "evaluation job",
		SpanName:         "get-evaluation-job",
		TraceIDAttribute: "job.id",
	}
}

func (Workload) ToEvaluationJob(request any) (*api.EvaluationJobConfig, error) {
	job, ok := request.(*api.EvaluationJobConfig)
	if !ok || job == nil {
		return nil, fmt.Errorf("invalid evaluation request type %T", request)
	}
	return job, nil
}

func (Workload) ResourceFromJob(job *api.EvaluationJobResource) (any, error) {
	return job, nil
}

func (Workload) ValidateCreate(job *api.EvaluationJobConfig) error {
	if job == nil {
		return serviceerrors.NewServiceError(messages.RequestValidationFailed, "Error", "evaluation configuration is required")
	}
	benchmarks := job.Benchmarks
	if job.Collection != nil {
		benchmarks = append(append([]api.EvaluationBenchmarkConfig(nil), benchmarks...), job.Collection.Benchmarks...)
	}
	for _, benchmark := range benchmarks {
		if workloads.IsInternalProviderID(benchmark.ProviderID) {
			return serviceerrors.NewServiceError(messages.ResourceDoesNotExist, "Type", "provider", "ResourceID", benchmark.ProviderID)
		}
	}
	return nil
}

func (w Workload) ValidateGet(id string, job *api.EvaluationJobResource) error {
	return workloads.ValidateResource(w, id, job)
}

func (w Workload) ValidateDelete(id string, job *api.EvaluationJobResource) error {
	return workloads.ValidateResource(w, id, job)
}
