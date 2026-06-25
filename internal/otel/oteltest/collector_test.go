package oteltest

import (
	"context"
	"testing"

	collpb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	mpb "go.opentelemetry.io/proto/otlp/metrics/v1"
)

func TestGRPCCollectorExportAndSnapshot(t *testing.T) {
	collector, err := NewGRPCCollector()
	if err != nil {
		t.Fatalf("NewGRPCCollector: %v", err)
	}
	t.Cleanup(collector.Shutdown)

	if collector.Endpoint() == "" {
		t.Fatal("expected non-empty endpoint")
	}

	ctx := context.Background()
	metric := &mpb.ResourceMetrics{
		ScopeMetrics: []*mpb.ScopeMetrics{
			{
				Metrics: []*mpb.Metric{
					{Name: "test.metric"},
				},
			},
		},
	}

	resp, err := collector.Export(ctx, &collpb.ExportMetricsServiceRequest{
		ResourceMetrics: []*mpb.ResourceMetrics{metric},
	})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}

	got := collector.ResourceMetrics()
	if len(got) != 1 {
		t.Fatalf("ResourceMetrics len = %d, want 1", len(got))
	}
	if got[0].GetScopeMetrics()[0].GetMetrics()[0].GetName() != "test.metric" {
		t.Fatalf("unexpected metric name %q", got[0].GetScopeMetrics()[0].GetMetrics()[0].GetName())
	}
}

func TestGRPCCollectorExportNilRequest(t *testing.T) {
	collector, err := NewGRPCCollector()
	if err != nil {
		t.Fatalf("NewGRPCCollector: %v", err)
	}
	t.Cleanup(collector.Shutdown)

	resp, err := collector.Export(context.Background(), nil)
	if err != nil {
		t.Fatalf("Export nil request: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response for nil request")
	}
	if len(collector.ResourceMetrics()) != 0 {
		t.Fatal("expected no stored metrics for nil request")
	}
}
