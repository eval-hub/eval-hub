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
		{Ref: api.Ref{ID: "judge-benchmark"}, ProviderID: "inspect", Parameters: map[string]any{"judge_model": "", "judge_route": ""}},
		{Ref: api.Ref{ID: "ordinary-benchmark"}, ProviderID: "inspect"},
	}}}
	modelWithSecret := &api.ModelRef{Auth: &api.ModelAuth{SecretRef: "model-credentials"}}
	benchmark := func(parameters map[string]any) []api.EvaluationBenchmarkConfig {
		return []api.EvaluationBenchmarkConfig{{Ref: api.Ref{ID: "judge-benchmark"}, ProviderID: "inspect", Parameters: parameters}}
	}

	tests := []struct {
		name       string
		collection *api.CollectionResource
		benchmarks []api.EvaluationBenchmarkConfig
		model      *api.ModelRef
		wantError  string
	}{
		{name: "no collection", benchmarks: benchmark(nil)},
		{name: "target route without secret", collection: marked, benchmarks: benchmark(map[string]any{"judge_model": "deployed-model", "judge_route": "target"})},
		{name: "judge route with secret", collection: marked, benchmarks: benchmark(map[string]any{"judge_model": "deployed-model", "judge_route": "judge"}), model: modelWithSecret},
		{name: "unmarked benchmark", collection: marked, benchmarks: []api.EvaluationBenchmarkConfig{{Ref: api.Ref{ID: "ordinary-benchmark"}, ProviderID: "inspect"}}},
		{name: "same ID wrong provider", collection: marked, benchmarks: []api.EvaluationBenchmarkConfig{{Ref: api.Ref{ID: "judge-benchmark"}, ProviderID: "other"}}},
		{name: "missing model", collection: marked, benchmarks: benchmark(map[string]any{"judge_route": "target"}), wantError: "judge_model"},
		{name: "blank model", collection: marked, benchmarks: benchmark(map[string]any{"judge_model": "  ", "judge_route": "target"}), wantError: "judge_model"},
		{name: "non-string model", collection: marked, benchmarks: benchmark(map[string]any{"judge_model": 123, "judge_route": "target"}), wantError: "judge_model"},
		{name: "missing route", collection: marked, benchmarks: benchmark(map[string]any{"judge_model": "deployed-model"}), wantError: "judge_route"},
		{name: "invalid route", collection: marked, benchmarks: benchmark(map[string]any{"judge_model": "deployed-model", "judge_route": "external"}), wantError: "judge_route"},
		{name: "non-string route", collection: marked, benchmarks: benchmark(map[string]any{"judge_model": "deployed-model", "judge_route": true}), wantError: "judge_route"},
		{name: "judge route without secret", collection: marked, benchmarks: benchmark(map[string]any{"judge_model": "deployed-model", "judge_route": "judge"}), wantError: "model.auth.secret_ref"},
		{name: "judge route with blank secret", collection: marked, benchmarks: benchmark(map[string]any{"judge_model": "deployed-model", "judge_route": "judge"}), model: &api.ModelRef{Auth: &api.ModelAuth{SecretRef: " "}}, wantError: "model.auth.secret_ref"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCollectionJudgeParameters(tt.collection, tt.benchmarks, tt.model)
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
