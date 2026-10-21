package api

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestPostProcessingResourceResultsJSON(t *testing.T) {
	for _, tc := range []struct {
		name    string
		results *PostProcessingResults
		want    string
	}{
		{name: "pending"},
		{
			name:    "external source without metadata",
			results: &PostProcessingResults{ConfidenceInterval: &ConfidenceInterval{Lower: 0, Upper: 1}},
			want:    `{"confidence_interval":{"lower":0,"upper":1}}`,
		},
		{
			name: "external source with partial metadata",
			results: &PostProcessingResults{ConfidenceInterval: &ConfidenceInterval{
				Lower: -0.1, Upper: 0.1, PointEstimate: new(0.0),
			}},
			want: `{"confidence_interval":{"lower":-0.1,"upper":0.1,"point_estimate":0}}`,
		},
		{
			name: "external source with zero point estimate",
			results: &PostProcessingResults{ConfidenceInterval: &ConfidenceInterval{
				Lower: -0.1, Upper: 0.1, PointEstimate: new(0.0),
				NLabeled: new(20), NUnlabeled: new(200), Alpha: new(0.05), Method: new("original_ppi"), Lambda: new(1.0),
			}},
			want: `{"confidence_interval":{"lower":-0.1,"upper":0.1,"point_estimate":0,"n_labeled":20,"n_unlabeled":200,"alpha":0.05,"method":"original_ppi","lambda":1}}`,
		},
		{
			name: "evaluation source",
			results: &PostProcessingResults{Benchmarks: []PostProcessingBenchmarkResult{{
				ID: "accuracy", ProviderID: "source-provider", BenchmarkIndex: 0,
				ConfidenceInterval: ConfidenceInterval{
					Lower: 0.75, Upper: 0.85, PointEstimate: new(0.8),
					NLabeled: new(100), NUnlabeled: new(1000), Alpha: new(0.05), Method: new("original_ppi"), Lambda: new(1.0),
				},
			}}},
			want: `{"benchmarks":[{"id":"accuracy","provider_id":"source-provider","benchmark_index":0,"confidence_interval":{"lower":0.75,"upper":0.85,"point_estimate":0.8,"n_labeled":100,"n_unlabeled":1000,"alpha":0.05,"method":"original_ppi","lambda":1}}]}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(PostProcessingResource{Results: tc.results})
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(data, &fields); err != nil {
				t.Fatal(err)
			}
			if tc.want == "" {
				if _, ok := fields["results"]; ok {
					t.Fatal("pending resource must omit results")
				}
				return
			}
			var got, want any
			if err := json.Unmarshal(fields["results"], &got); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(tc.want), &want); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("results=%s, want %s", fields["results"], tc.want)
			}
			var resource PostProcessingResource
			if err := json.Unmarshal(data, &resource); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(resource.Results, tc.results) {
				t.Fatal("results did not round trip")
			}
		})
	}
}

func TestPostProcessingOperations(t *testing.T) {
	tests := []struct {
		name       string
		standalone StandalonePostProcessingOperations
		job        JobPostProcessingOperations
		wantNames  []string
	}{
		{name: "no operations"},
		{
			name: "confidence interval",
			standalone: StandalonePostProcessingOperations{
				ConfidenceInterval: &StandaloneConfidenceIntervalConfig{},
			},
			job: JobPostProcessingOperations{
				ConfidenceInterval: &ConfidenceIntervalConfig{},
			},
			wantNames: []string{"confidence_interval"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			wantOperation := len(test.wantNames) > 0
			if got := test.standalone.HasOperation(); got != wantOperation {
				t.Errorf("StandalonePostProcessingOperations.HasOperation() = %t, want %t", got, wantOperation)
			}
			if got := test.job.HasOperation(); got != wantOperation {
				t.Errorf("JobPostProcessingOperations.HasOperation() = %t, want %t", got, wantOperation)
			}
			if got := test.standalone.OperationNames(); !reflect.DeepEqual(got, test.wantNames) {
				t.Errorf("StandalonePostProcessingOperations.OperationNames() = %v, want %v", got, test.wantNames)
			}
			if got := test.job.OperationNames(); !reflect.DeepEqual(got, test.wantNames) {
				t.Errorf("JobPostProcessingOperations.OperationNames() = %v, want %v", got, test.wantNames)
			}
		})
	}
}
