package handlers

import (
	"context"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/metrics"
	"github.com/eval-hub/eval-hub/pkg/api"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestRecordEvaluationJobTerminalStateAfterUpdate(t *testing.T) {
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	otel.SetMeterProvider(provider)

	if err := metrics.Init(); err != nil {
		t.Fatalf("metrics.Init: %v", err)
	}

	recordEvaluationJobTerminalStateAfterUpdate(
		context.Background(),
		func() (*api.EvaluationJobResource, error) {
			return &api.EvaluationJobResource{
				Status: &api.EvaluationJobStatus{
					EvaluationJobState: api.EvaluationJobState{State: api.OverallStateCompleted},
				},
			}, nil
		},
		api.OverallStateRunning,
	)

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}

	found := false
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == "evalhub.evaluation_job_completions" {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("expected evalhub.evaluation_job_completions metric")
	}
}
