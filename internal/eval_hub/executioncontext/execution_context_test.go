package executioncontext_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/executioncontext"
	"github.com/eval-hub/eval-hub/pkg/api"
)

func TestNewExecutionContext(t *testing.T) {
	ctx := context.Background()
	logger := slog.Default()
	user := api.User("u")
	tenant := api.Tenant("t")
	ec := executioncontext.NewExecutionContext(ctx, "req-1", logger, user, tenant)
	if ec.Ctx != ctx || ec.RequestID != "req-1" || ec.Logger != logger {
		t.Fatalf("unexpected fields on ExecutionContext: %#v", ec)
	}
	if ec.User != user || ec.Tenant != tenant {
		t.Fatalf("user/tenant: %q / %q", ec.User, ec.Tenant)
	}
	if ec.StartedAt.IsZero() {
		t.Error("StartedAt should be set")
	}
}

func TestExecutionContext_WithContext(t *testing.T) {
	base := context.Background()
	next, cancel := context.WithCancel(base)
	defer cancel()

	ec := executioncontext.NewExecutionContext(base, "id", slog.Default(), api.User(""), api.Tenant(""))
	wrapped := ec.WithContext(next)
	if wrapped.Ctx != next {
		t.Error("WithContext should replace Ctx")
	}
	if wrapped.RequestID != ec.RequestID || wrapped.Logger != ec.Logger || wrapped.StartedAt != ec.StartedAt {
		t.Error("WithContext should preserve other fields")
	}
}
