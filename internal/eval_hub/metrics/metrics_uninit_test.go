package metrics

import (
	"context"
	"net/http"
	"testing"

	"github.com/eval-hub/eval-hub/pkg/api"
	"go.opentelemetry.io/otel/attribute"
)

func TestWithTenantAttr(t *testing.T) {
	t.Run("empty tenant leaves attrs unchanged", func(t *testing.T) {
		attrs := []attribute.KeyValue{attribute.String("k", "v")}
		got := withTenantAttr(attrs, "")
		if len(got) != 1 {
			t.Fatalf("withTenantAttr with empty tenant = %v, want unchanged", got)
		}
	})

	t.Run("non-empty tenant is appended", func(t *testing.T) {
		attrs := []attribute.KeyValue{attribute.String("k", "v")}
		got := withTenantAttr(attrs, "tenant-a")
		if len(got) != 2 {
			t.Fatalf("withTenantAttr with tenant = %v, want 2 attrs", got)
		}
		if got[1].Key != "tenant" || got[1].Value.AsString() != "tenant-a" {
			t.Errorf("withTenantAttr appended = %v, want tenant=tenant-a", got[1])
		}
	})

	t.Run("nil base attrs with empty tenant returns nil", func(t *testing.T) {
		got := withTenantAttr(nil, "")
		if got != nil {
			t.Errorf("withTenantAttr(nil, \"\") = %v, want nil", got)
		}
	})
}

func TestRecordMetricsBeforeInitNoPanic(t *testing.T) {
	ctx := context.Background()

	RecordEvaluationJobCreated(ctx, "local", "tenant-a")
	RecordEvaluationJobCancelled(ctx, "tenant-a")
	RecordEvaluationJobRuntimeStartFailed(ctx, "kubernetes", "tenant-a")
	RecordEvaluationJobTerminalState(ctx, api.OverallStateRunning, api.OverallStateCompleted, "tenant-a")
	RecordBenchmarkRuntimeError(ctx, "local", "tenant-a")
	RecordHTTPServerRequest(ctx, http.MethodGet, "/health", http.StatusOK)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "/health", nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext: %v", err)
	}
	IncHTTPServerActiveRequests(ctx, req)
	DecHTTPServerActiveRequests(ctx, req)

	RecordEvaluationJobStateTransition(ctx, "prov", "coll", "pending", "tenant-a")
	ObserveEvaluationJobDuration(ctx, "prov", "coll", 10.5, "tenant-a")
	IncActiveJobs(ctx, "tenant-a")
	DecActiveJobs(ctx, "tenant-a")
	IncQueueDepth(ctx, "tenant-a")
	DecQueueDepth(ctx, "tenant-a")
	RecordEvaluationError(ctx, "k8s_create_failed", "prov", "tenant-a")
	ObserveBenchmarkDuration(ctx, "mmlu", "prov", 42.0, "tenant-a")
	RecordBenchmarkCompletion(ctx, "mmlu", "prov", "completed", "tenant-a")
	ObserveAPIRequestDuration(ctx, "/api/v1/health", "GET", "", "", 0.05, "tenant-a")
}
