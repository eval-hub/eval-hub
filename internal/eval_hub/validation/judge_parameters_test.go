package validation

import (
	"errors"
	"strings"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/messages"
	"github.com/eval-hub/eval-hub/internal/eval_hub/serviceerrors"
	"github.com/eval-hub/eval-hub/pkg/api"
)

func TestValidateCollectionJudgeParameters(t *testing.T) {
	marked := &api.CollectionResource{CollectionConfig: api.CollectionConfig{Benchmarks: []api.CollectionBenchmarkConfig{
		{Ref: api.Ref{ID: "judge-benchmark"}, ProviderID: "inspect", Parameters: map[string]any{"judge_model": ""}},
		{Ref: api.Ref{ID: "ordinary-benchmark"}, ProviderID: "inspect"},
	}}}
	benchmark := func(parameters map[string]any) []api.EvaluationBenchmarkConfig {
		return []api.EvaluationBenchmarkConfig{{Ref: api.Ref{ID: "judge-benchmark"}, ProviderID: "inspect", Parameters: parameters}}
	}

	tests := []struct {
		name       string
		collection *api.CollectionResource
		benchmarks []api.EvaluationBenchmarkConfig
		wantError  string
	}{
		{name: "no collection", benchmarks: benchmark(nil)},
		{name: "judge model without secret", collection: marked, benchmarks: benchmark(map[string]any{"judge_model": "deployed-model"})},
		{name: "unmarked benchmark", collection: marked, benchmarks: []api.EvaluationBenchmarkConfig{{Ref: api.Ref{ID: "ordinary-benchmark"}, ProviderID: "inspect"}}},
		{name: "same ID wrong provider", collection: marked, benchmarks: []api.EvaluationBenchmarkConfig{{Ref: api.Ref{ID: "judge-benchmark"}, ProviderID: "other"}}},
		{name: "missing model", collection: marked, benchmarks: benchmark(nil), wantError: "judge_model"},
		{name: "blank model", collection: marked, benchmarks: benchmark(map[string]any{"judge_model": "  "}), wantError: "judge_model"},
		{name: "non-string model", collection: marked, benchmarks: benchmark(map[string]any{"judge_model": 123}), wantError: "judge_model"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCollectionJudgeParameters(tt.collection, tt.benchmarks)
			if tt.wantError == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			var serviceErr *serviceerrors.ServiceError
			if !errors.As(err, &serviceErr) || serviceErr.MessageCode() != messages.RequestValidationFailed || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("error = %v, want RequestValidationFailed mentioning %q", err, tt.wantError)
			}
		})
	}
}
