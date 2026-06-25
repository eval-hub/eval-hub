package oteltest

import (
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	mpb "go.opentelemetry.io/proto/otlp/metrics/v1"
)

// MetricNames returns the set of metric names present in exported OTLP data.
func MetricNames(resourceMetrics []*mpb.ResourceMetrics) map[string]struct{} {
	names := make(map[string]struct{})
	for _, rm := range resourceMetrics {
		for _, sm := range rm.GetScopeMetrics() {
			for _, m := range sm.GetMetrics() {
				if name := m.GetName(); name != "" {
					names[name] = struct{}{}
				}
			}
		}
	}
	return names
}

// HasIntSumDataPoint reports whether a sum metric has an integer data point with the
// given attribute key/value pair.
func HasIntSumDataPoint(resourceMetrics []*mpb.ResourceMetrics, metricName, attrKey, attrValue string) bool {
	for _, rm := range resourceMetrics {
		for _, sm := range rm.GetScopeMetrics() {
			for _, m := range sm.GetMetrics() {
				if m.GetName() != metricName {
					continue
				}
				sum := m.GetSum()
				if sum == nil {
					continue
				}
				for _, dp := range sum.GetDataPoints() {
					if !hasAttribute(dp.GetAttributes(), attrKey, attrValue) {
						continue
					}
					if dp.GetAsInt() > 0 {
						return true
					}
				}
			}
		}
	}
	return false
}

func hasAttribute(attrs []*commonpb.KeyValue, key, value string) bool {
	for _, attr := range attrs {
		if attr.GetKey() != key {
			continue
		}
		if attr.GetValue().GetStringValue() == value {
			return true
		}
	}
	return false
}
