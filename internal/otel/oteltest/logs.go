package oteltest

import (
	"context"
	"net"
	"strings"
	"sync"

	colllogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	colltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	lpb "go.opentelemetry.io/proto/otlp/logs/v1"
	"google.golang.org/grpc"
)

// GRPCLogsCollector is a minimal OTLP gRPC receiver for tests. It captures log
// records exported via the OTLP LogsService and accepts trace exports as a
// no-op, so a single endpoint can serve adapters that export both logs and
// spans (as the eval-hub Python adapter does).
type GRPCLogsCollector struct {
	mu   sync.Mutex
	logs []*lpb.ResourceLogs

	listener net.Listener
	server   *grpc.Server
}

// NewGRPCLogsCollector listens on an ephemeral localhost port.
func NewGRPCLogsCollector() (*GRPCLogsCollector, error) {
	var lc net.ListenConfig
	listener, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}

	c := &GRPCLogsCollector{
		listener: listener,
		server:   grpc.NewServer(),
	}
	colllogspb.RegisterLogsServiceServer(c.server, &logsService{c: c})
	// Register a no-op trace receiver so span exports from the same endpoint do
	// not fail with Unimplemented (the adapter wraps the run in a root span).
	colltracepb.RegisterTraceServiceServer(c.server, &traceService{})
	go func() { _ = c.server.Serve(listener) }()
	return c, nil
}

// Endpoint returns the collector address in host:port form for OTLP gRPC exporters.
func (c *GRPCLogsCollector) Endpoint() string {
	return c.listener.Addr().String()
}

// Shutdown stops the collector gRPC server.
func (c *GRPCLogsCollector) Shutdown() {
	c.server.Stop()
}

// ResourceLogs returns a snapshot of received resource logs.
func (c *GRPCLogsCollector) ResourceLogs() []*lpb.ResourceLogs {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]*lpb.ResourceLogs, len(c.logs))
	copy(out, c.logs)
	return out
}

// LogRecords returns a flattened snapshot of every received log record.
func (c *GRPCLogsCollector) LogRecords() []*lpb.LogRecord {
	var records []*lpb.LogRecord
	for _, rl := range c.ResourceLogs() {
		for _, sl := range rl.GetScopeLogs() {
			records = append(records, sl.GetLogRecords()...)
		}
	}
	return records
}

type logsService struct {
	colllogspb.UnimplementedLogsServiceServer
	c *GRPCLogsCollector
}

// Export implements the OTLP logs collector service.
func (s *logsService) Export(_ context.Context, req *colllogspb.ExportLogsServiceRequest) (*colllogspb.ExportLogsServiceResponse, error) {
	if req == nil {
		return &colllogspb.ExportLogsServiceResponse{}, nil
	}
	s.c.mu.Lock()
	s.c.logs = append(s.c.logs, req.ResourceLogs...)
	s.c.mu.Unlock()
	return &colllogspb.ExportLogsServiceResponse{}, nil
}

type traceService struct {
	colltracepb.UnimplementedTraceServiceServer
}

// Export accepts and discards trace exports.
func (s *traceService) Export(_ context.Context, _ *colltracepb.ExportTraceServiceRequest) (*colltracepb.ExportTraceServiceResponse, error) {
	return &colltracepb.ExportTraceServiceResponse{}, nil
}

// FindLogRecordByBody returns the first log record whose (string) body contains
// substr, or nil when none match.
func FindLogRecordByBody(records []*lpb.LogRecord, substr string) *lpb.LogRecord {
	for _, r := range records {
		if strings.Contains(r.GetBody().GetStringValue(), substr) {
			return r
		}
	}
	return nil
}

// LogRecordAttribute returns the string value of the attribute with the given
// key on the record, and whether it was present.
func LogRecordAttribute(record *lpb.LogRecord, key string) (string, bool) {
	return attributeValue(record.GetAttributes(), key)
}

func attributeValue(attrs []*commonpb.KeyValue, key string) (string, bool) {
	for _, attr := range attrs {
		if attr.GetKey() == key {
			return attr.GetValue().GetStringValue(), true
		}
	}
	return "", false
}
