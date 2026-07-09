package handlers

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
	"github.com/eval-hub/eval-hub/internal/eval_hub/executioncontext"
)

func TestWithSpanOddAttributeCount(t *testing.T) {
	t.Parallel()

	h := New(nil, nil, nil, nil, &config.Config{}, nil)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-1", logger, "user", "tenant")

	err := h.withSpan(ctx, func(context.Context) error { return nil }, "test", "op", "key", "value", "orphan")
	if err != nil {
		t.Fatalf("withSpan() = %v, want nil", err)
	}
}
