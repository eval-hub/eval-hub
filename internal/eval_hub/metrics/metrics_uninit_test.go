package metrics

import (
	"context"
	"net/http"
	"testing"

	"github.com/eval-hub/eval-hub/pkg/api"
)

func TestRecordMetricsBeforeInitNoPanic(t *testing.T) {
	ctx := context.Background()

	RecordEvaluationJobCreated(ctx, "local")
	RecordEvaluationJobCancelled(ctx)
	RecordEvaluationJobRuntimeStartFailed(ctx, "kubernetes")
	RecordEvaluationJobTerminalState(ctx, api.OverallStateRunning, api.OverallStateCompleted)
	RecordBenchmarkRuntimeError(ctx, "local")
	RecordHTTPServerRequest(ctx, http.MethodGet, "/health", http.StatusOK)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "/health", nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext: %v", err)
	}
	IncHTTPServerActiveRequests(ctx, req)
	DecHTTPServerActiveRequests(ctx, req)

	RecordEvaluationJobStateTransition(ctx, "prov", "coll", "pending")
	ObserveEvaluationJobDuration(ctx, "prov", "coll", 10.5)
	IncActiveJobs(ctx)
	DecActiveJobs(ctx)
	IncQueueDepth(ctx)
	DecQueueDepth(ctx)
	RecordEvaluationError(ctx, "k8s_create_failed", "prov")
	ObserveBenchmarkDuration(ctx, "mmlu", "prov", 42.0)
	ObserveAPIRequestDuration(ctx, "/api/v1/health", "GET", "", "", 0.05)
}
