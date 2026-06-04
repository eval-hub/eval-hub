package serialization

import (
	"context"
	"log/slog"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/executioncontext"
	"github.com/eval-hub/eval-hub/internal/eval_hub/validation"
	"github.com/eval-hub/eval-hub/pkg/api"
)

func TestUnmarshal_TrimsQueueNameBeforeValidation(t *testing.T) {
	validate := validation.NewValidator()
	ctx := executioncontext.NewExecutionContext(
		context.Background(),
		"req-trim-queue",
		slog.New(slog.NewTextHandler(nil, nil)),
		"test-user",
		"test-tenant",
	)
	body := []byte(`{
		"name": "test-evaluation-job-queue",
		"model": {"url": "http://test.com", "name": "test"},
		"benchmarks": [{"id": "arc_easy", "provider_id": "lm_evaluation_harness"}],
		"queue": {"kind": "kueue", "name": "  user-queue  "}
	}`)

	var cfg api.EvaluationJobConfig
	if err := Unmarshal(validate, ctx, body, &cfg, api.NormalizeEvaluationJobConfig); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if cfg.Queue == nil || cfg.Queue.Name != "user-queue" || cfg.Queue.Kind != "kueue" {
		t.Fatalf("got queue %+v", cfg.Queue)
	}
}
