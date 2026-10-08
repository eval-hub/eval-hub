package postprocessing

import (
	"strings"
	"testing"

	"github.com/eval-hub/eval-hub/pkg/api"
)

func TestIsPostProcessingJob(t *testing.T) {
	valid := api.EvaluationJobConfig{Benchmarks: []api.EvaluationBenchmarkConfig{{
		ProviderID: ProviderID,
		Ref:        api.Ref{ID: BenchmarkID},
	}}}
	tests := []struct {
		name string
		cfg  *api.EvaluationJobConfig
		want bool
	}{
		{name: "nil config"},
		{name: "no benchmarks", cfg: &api.EvaluationJobConfig{}},
		{name: "multiple benchmarks", cfg: &api.EvaluationJobConfig{Benchmarks: append(valid.Benchmarks, valid.Benchmarks[0])}},
		{name: "collection job", cfg: &api.EvaluationJobConfig{Collection: &api.CollectionRef{ID: "collection"}, Benchmarks: valid.Benchmarks}},
		{name: "other provider", cfg: &api.EvaluationJobConfig{Benchmarks: []api.EvaluationBenchmarkConfig{{ProviderID: "other", Ref: api.Ref{ID: BenchmarkID}}}}},
		{name: "other benchmark", cfg: &api.EvaluationJobConfig{Benchmarks: []api.EvaluationBenchmarkConfig{{ProviderID: ProviderID, Ref: api.Ref{ID: "other"}}}}},
		{name: "post-processing job", cfg: &valid, want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := IsPostProcessingJob(test.cfg); got != test.want {
				t.Fatalf("IsPostProcessingJob() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestToEvaluationJob(t *testing.T) {
	operations := api.StandalonePostProcessingOperations{ConfidenceInterval: &api.StandaloneConfidenceIntervalConfig{}}
	hardware := &api.BenchmarkHardwareConfig{}
	tests := []struct {
		name         string
		request      api.StandalonePostProcessingRequest
		wantName     string
		wantHasOrder bool
	}{
		{name: "default name", request: api.StandalonePostProcessingRequest{Operations: operations}, wantName: "post-processing"},
		{
			name: "preserves request fields",
			request: api.StandalonePostProcessingRequest{
				PostProcessingCommon: api.PostProcessingCommon{Name: "requested name", HardwareConfig: hardware, OperationOrder: []string{"confidence_interval"}},
				Operations:           operations,
			},
			wantName:     "requested name",
			wantHasOrder: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			job := ToEvaluationJob(&test.request)
			if job.Name != test.wantName || job.Model == nil || job.Model.Name != BenchmarkID || job.HardwareConfig != test.request.HardwareConfig {
				t.Fatalf("mapped job has unexpected fields: %+v", job)
			}
			if len(job.Benchmarks) != 1 || job.Benchmarks[0].ProviderID != ProviderID || job.Benchmarks[0].ID != BenchmarkID {
				t.Fatalf("mapped benchmark is incorrect: %+v", job.Benchmarks)
			}
			if _, ok := job.Benchmarks[0].Parameters["operation_order"]; ok != test.wantHasOrder {
				t.Errorf("operation_order present = %t, want %t", ok, test.wantHasOrder)
			}
			if _, ok := job.Benchmarks[0].Parameters["operations"]; !ok {
				t.Error("mapped parameters do not contain operations")
			}
		})
	}
}

func TestOperationsFromJob(t *testing.T) {
	tests := []struct {
		name       string
		job        *api.EvaluationJobConfig
		wantErr    string
		wantConfig bool
	}{
		{name: "not a post-processing job", job: &api.EvaluationJobConfig{}, wantErr: "not a post-processing computation"},
		{name: "missing operations", job: postProcessingJob(nil), wantErr: "has no operations"},
		{name: "marshal error", job: postProcessingJob(make(chan int)), wantErr: "marshal post-processing operations"},
		{name: "decode error", job: postProcessingJob(map[string]any{"operations": map[string]any{"confidence_interval": "invalid"}}), wantErr: "decode post-processing operations"},
		{name: "no supported operation", job: postProcessingJob(map[string]any{"operations": map[string]any{"future_operation": map[string]any{}}}), wantErr: "has no supported operations"},
		{name: "valid operation", job: postProcessingJob(map[string]any{"operations": validOperations()}), wantConfig: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			operations, err := OperationsFromJob(test.job)
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("OperationsFromJob() error = %v, want it to contain %q", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("OperationsFromJob() error = %v", err)
			}
			if got := operations.HasOperation(); got != test.wantConfig {
				t.Fatalf("HasOperation() = %t, want %t", got, test.wantConfig)
			}
		})
	}
}

func TestOperationOrderFromJob(t *testing.T) {
	tests := []struct {
		name    string
		job     *api.EvaluationJobConfig
		want    []string
		wantErr string
	}{
		{name: "not a post-processing job", job: &api.EvaluationJobConfig{}, wantErr: "not a post-processing computation"},
		{name: "order omitted", job: postProcessingJob(map[string]any{"operations": validOperations()})},
		{name: "marshal error", job: postProcessingJob(map[string]any{"operation_order": make(chan int)}), wantErr: "marshal post-processing operation order"},
		{name: "decode error", job: postProcessingJob(map[string]any{"operation_order": 1}), wantErr: "decode post-processing operation order"},
		{name: "order present", job: postProcessingJob(map[string]any{"operation_order": []any{"confidence_interval"}}), want: []string{"confidence_interval"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := OperationOrderFromJob(test.job)
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("OperationOrderFromJob() error = %v, want it to contain %q", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("OperationOrderFromJob() error = %v", err)
			}
			if len(got) != len(test.want) {
				t.Fatalf("OperationOrderFromJob() = %v, want %v", got, test.want)
			}
			for i := range got {
				if got[i] != test.want[i] {
					t.Fatalf("OperationOrderFromJob() = %v, want %v", got, test.want)
				}
			}
		})
	}
}

func TestResourceFromJob(t *testing.T) {
	valid := postProcessingJob(map[string]any{
		"operations":      validOperations(),
		"operation_order": []any{"confidence_interval"},
	})
	namedValid := *valid
	namedValid.Name = "mapped name"
	tests := []struct {
		name       string
		job        *api.EvaluationJobResource
		wantErr    string
		wantStatus bool
	}{
		{name: "invalid computation", job: &api.EvaluationJobResource{EvaluationJobConfig: api.EvaluationJobConfig{}}, wantErr: "not a post-processing computation"},
		{name: "invalid operation order", job: &api.EvaluationJobResource{EvaluationJobConfig: *postProcessingJob(map[string]any{"operations": validOperations(), "operation_order": 1})}, wantErr: "decode post-processing operation order"},
		{name: "no status", job: &api.EvaluationJobResource{EvaluationJobConfig: *valid}},
		{name: "status without one benchmark", job: &api.EvaluationJobResource{EvaluationJobConfig: *valid, Status: &api.EvaluationJobStatus{EvaluationJobState: api.EvaluationJobState{State: api.OverallStateRunning}, Benchmarks: []api.BenchmarkStatus{{}, {}}}}, wantStatus: true},
		{name: "status with one benchmark", job: &api.EvaluationJobResource{EvaluationJobConfig: namedValid, Status: &api.EvaluationJobStatus{EvaluationJobState: api.EvaluationJobState{State: api.OverallStateFailed}, Benchmarks: []api.BenchmarkStatus{{ErrorMessage: &api.MessageInfo{Message: "failed"}, WarningMessage: &api.MessageInfo{Message: "warning"}}}}}, wantStatus: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resource, err := ResourceFromJob(test.job)
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("ResourceFromJob() error = %v, want it to contain %q", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ResourceFromJob() error = %v", err)
			}
			if (resource.Status.State != api.StatePending) && !test.wantStatus {
				t.Fatalf("status state = %s, want pending", resource.Status.State)
			}
			if test.wantStatus && len(test.job.Status.Benchmarks) == 1 {
				if resource.Status.State != api.StateFailed || resource.Name != "mapped name" || resource.Status.ErrorMessage == nil || resource.Status.WarningMessage == nil {
					t.Fatalf("status metadata was not copied: %+v", resource.Status)
				}
			}
		})
	}
}

func postProcessingJob(parameters any) *api.EvaluationJobConfig {
	var params map[string]any
	if parameters != nil {
		if mapped, ok := parameters.(map[string]any); ok {
			params = mapped
		} else {
			params = map[string]any{"operations": parameters}
		}
	}
	return &api.EvaluationJobConfig{Benchmarks: []api.EvaluationBenchmarkConfig{{ProviderID: ProviderID, Ref: api.Ref{ID: BenchmarkID}, Parameters: params}}}
}

func validOperations() map[string]any {
	return map[string]any{"confidence_interval": map[string]any{
		"results_data_ref": map[string]any{"eval_job": map[string]any{"id": "source-job"}},
		"calibration_data_ref": []any{map[string]any{
			"pvc": map[string]any{"claim_name": "calibration-data"},
			"data_config": map[string]any{
				"format":  "jsonl",
				"columns": map[string]any{"label": "label", "prediction": "prediction"},
			},
		}},
		"significance_level": 0.05,
	}}
}
