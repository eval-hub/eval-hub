// Package oteltest provides test helpers for OpenTelemetry export verification.
package oteltest

import (
	"context"
	"net"
	"sync"

	collpb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	mpb "go.opentelemetry.io/proto/otlp/metrics/v1"
	"google.golang.org/grpc"
)

// GRPCCollector is a minimal OTLP gRPC metrics receiver for tests.
type GRPCCollector struct {
	collpb.UnimplementedMetricsServiceServer

	mu   sync.Mutex
	data []*mpb.ResourceMetrics

	listener net.Listener
	server   *grpc.Server
}

// NewGRPCCollector listens on an ephemeral localhost port.
func NewGRPCCollector() (*GRPCCollector, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}

	c := &GRPCCollector{
		listener: listener,
		server:   grpc.NewServer(),
	}
	collpb.RegisterMetricsServiceServer(c.server, c)
	go func() { _ = c.server.Serve(listener) }()
	return c, nil
}

// Endpoint returns the collector address in host:port form for OTLP gRPC exporters.
func (c *GRPCCollector) Endpoint() string {
	return c.listener.Addr().String()
}

// Shutdown stops the collector gRPC server.
func (c *GRPCCollector) Shutdown() {
	c.server.Stop()
}

// Export implements the OTLP metrics collector service.
func (c *GRPCCollector) Export(_ context.Context, req *collpb.ExportMetricsServiceRequest) (*collpb.ExportMetricsServiceResponse, error) {
	if req == nil {
		return &collpb.ExportMetricsServiceResponse{}, nil
	}
	c.mu.Lock()
	c.data = append(c.data, req.ResourceMetrics...)
	c.mu.Unlock()
	return &collpb.ExportMetricsServiceResponse{}, nil
}

// ResourceMetrics returns a snapshot of received resource metrics.
func (c *GRPCCollector) ResourceMetrics() []*mpb.ResourceMetrics {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]*mpb.ResourceMetrics, len(c.data))
	copy(out, c.data)
	return out
}
