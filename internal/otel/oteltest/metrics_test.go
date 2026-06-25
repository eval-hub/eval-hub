package oteltest

import (
	"testing"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	mpb "go.opentelemetry.io/proto/otlp/metrics/v1"
)

func TestMetricNames(t *testing.T) {
	t.Parallel()

	data := []*mpb.ResourceMetrics{
		{
			ScopeMetrics: []*mpb.ScopeMetrics{
				{
					Metrics: []*mpb.Metric{
						{Name: "http.server.request.count"},
						{Name: ""},
						{Name: "evalhub.evaluation_jobs"},
					},
				},
			},
		},
	}

	names := MetricNames(data)
	for _, want := range []string{"http.server.request.count", "evalhub.evaluation_jobs"} {
		if _, ok := names[want]; !ok {
			t.Errorf("missing metric name %q", want)
		}
	}
	if _, ok := names[""]; ok {
		t.Error("empty metric name should be omitted")
	}
}

func TestHasIntSumDataPoint(t *testing.T) {
	t.Parallel()

	data := []*mpb.ResourceMetrics{
		{
			ScopeMetrics: []*mpb.ScopeMetrics{
				{
					Metrics: []*mpb.Metric{
						{
							Name: "http.server.request.count",
							Data: &mpb.Metric_Sum{
								Sum: &mpb.Sum{
									DataPoints: []*mpb.NumberDataPoint{
										{
											Value: &mpb.NumberDataPoint_AsInt{AsInt: 1},
											Attributes: []*commonpb.KeyValue{
												{
													Key: "http.route",
													Value: &commonpb.AnyValue{
														Value: &commonpb.AnyValue_StringValue{StringValue: "/api/v1/health"},
													},
												},
											},
										},
									},
								},
							},
						},
						{
							Name: "other.metric",
							Data: &mpb.Metric_Sum{
								Sum: &mpb.Sum{
									DataPoints: []*mpb.NumberDataPoint{
										{Value: &mpb.NumberDataPoint_AsInt{AsInt: 0}},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	if !HasIntSumDataPoint(data, "http.server.request.count", "http.route", "/api/v1/health") {
		t.Error("expected matching int sum data point")
	}
	if HasIntSumDataPoint(data, "http.server.request.count", "http.route", "/missing") {
		t.Error("unexpected match for wrong attribute value")
	}
	if HasIntSumDataPoint(data, "other.metric", "http.route", "/api/v1/health") {
		t.Error("unexpected match for zero value data point")
	}
	if HasIntSumDataPoint(data, "missing.metric", "http.route", "/api/v1/health") {
		t.Error("unexpected match for missing metric")
	}
}
