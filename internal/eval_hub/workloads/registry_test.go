package workloads_test

import (
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/evaluations"
	"github.com/eval-hub/eval-hub/internal/eval_hub/postprocessing"
	"github.com/eval-hub/eval-hub/internal/eval_hub/workloads"
	"github.com/eval-hub/eval-hub/pkg/api"
)

type customWorkload struct {
	evaluations.Workload
	workloadType workloads.Type
}

func (w customWorkload) Type() workloads.Type { return w.workloadType }
func (w customWorkload) ProviderID() string   { return string(w.workloadType) + "-provider" }
func (customWorkload) BenchmarkID() string    { return "custom-benchmark" }
func (customWorkload) Internal() bool         { return true }
func (w customWorkload) MatchesJob(job *api.EvaluationJobConfig) bool {
	return job != nil && job.Name == string(w.workloadType)
}

func TestRegistryAcceptsWorkloadImplementations(t *testing.T) {
	for _, pointer := range []bool{false, true} {
		name := "value"
		if pointer {
			name = "pointer"
		}
		t.Run(name, func(t *testing.T) {
			implementation := customWorkload{workloadType: workloads.Type(t.Name())}
			var workload workloads.Workload = implementation
			if pointer {
				workload = &implementation
			}
			workloads.Register(workload)
			if got := workloads.ByType(workload.Type()); got != workload {
				t.Fatalf("registry returned %v, want original implementation %v", got, workload)
			}
			job := &api.EvaluationJobConfig{Name: string(workload.Type())}
			if got := workloads.ForJob(job); got != workload {
				t.Fatalf("job resolved to %v, want %v", got, workload)
			}
			if !workloads.IsInternalProviderID(workload.ProviderID()) {
				t.Fatal("custom internal provider was not recognized")
			}
			// A duplicate implementation must not replace the original registration.
			workloads.Register(implementation)
			if got := workloads.ByType(workload.Type()); got != workload {
				t.Fatal("duplicate registration replaced the original implementation")
			}
		})
	}
}

func TestEvaluationIsRegistryFallback(t *testing.T) {
	if workload := workloads.ByType(workloads.Evaluation); workload == nil {
		t.Fatal("evaluation workload is not registered")
	}
	if got := workloads.ForJob(&api.EvaluationJobConfig{Name: "ordinary"}); got == nil || got.Type() != workloads.Evaluation {
		t.Fatalf("ordinary job resolved to %v", got)
	}
	postProcessingJob := postprocessing.ToEvaluationJob(&api.StandalonePostProcessingRequest{})
	if got := workloads.ForJob(postProcessingJob); got == nil || got.Type() != workloads.PostProcessing {
		t.Fatalf("post-processing job was hidden by the evaluation fallback: %v", got)
	}
	workloads.Register(nil)
}

func TestWorkloadOperationValidationRemainsSeparate(t *testing.T) {
	postProcessingJob := &api.EvaluationJobResource{EvaluationJobConfig: *postprocessing.ToEvaluationJob(&api.StandalonePostProcessingRequest{
		Operations: api.StandalonePostProcessingOperations{ConfidenceInterval: &api.StandaloneConfidenceIntervalConfig{}},
	})}
	evaluationJob := &api.EvaluationJobResource{EvaluationJobConfig: api.EvaluationJobConfig{Name: "ordinary"}}
	for _, test := range []struct {
		workload   workloads.Type
		own, other *api.EvaluationJobResource
	}{
		{workload: workloads.Evaluation, own: evaluationJob, other: postProcessingJob},
		{workload: workloads.PostProcessing, own: postProcessingJob, other: evaluationJob},
	} {
		t.Run(string(test.workload), func(t *testing.T) {
			workload := workloads.ByType(test.workload)
			if err := workload.ValidateCreate(&test.own.EvaluationJobConfig); err != nil {
				t.Fatalf("validate own create: %v", err)
			}
			if err := workload.ValidateCreate(&test.other.EvaluationJobConfig); err == nil {
				t.Fatal("create accepted another workload's configuration")
			}
			for _, validate := range []func(string, *api.EvaluationJobResource) error{workload.ValidateGet, workload.ValidateDelete} {
				if err := validate("own", test.own); err != nil {
					t.Fatalf("validate own resource: %v", err)
				}
				if err := validate("other", test.other); err == nil {
					t.Fatal("accepted another workload's resource")
				}
				if err := validate("missing", nil); err == nil {
					t.Fatal("accepted a missing resource")
				}
			}
		})
	}
}
