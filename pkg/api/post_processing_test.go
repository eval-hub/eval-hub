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
			name:    "external source",
			results: &PostProcessingResults{ConfidenceInterval: &ConfidenceInterval{Lower: 0, Upper: 1}},
			want:    `{"confidence_interval":{"lower":0,"upper":1}}`,
		},
		{
			name: "evaluation source",
			results: &PostProcessingResults{Benchmarks: []PostProcessingBenchmarkResult{{
				ID: "accuracy", ProviderID: "source-provider", BenchmarkIndex: 0,
				ConfidenceInterval: ConfidenceInterval{Lower: 0.75, Upper: 0.85},
			}}},
			want: `{"benchmarks":[{"id":"accuracy","provider_id":"source-provider","benchmark_index":0,"confidence_interval":{"lower":0.75,"upper":0.85}}]}`,
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
