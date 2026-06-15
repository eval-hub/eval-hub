package server

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/evalhub_mcp/config"
	"github.com/eval-hub/eval-hub/internal/testhelpers"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var discardLogger = slog.New(slog.DiscardHandler)

func connectServer(t *testing.T, info *ServerInfo) (context.Context, *mcp.ClientSession) {
	t.Helper()

	srv := New(info, discardLogger, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	return ctx, connectClient(t, ctx, srv)
}

// --- ServerInfo ---

func TestVersionString(t *testing.T) {
	t.Parallel()
	repoVersion := testhelpers.Version(t)
	tests := []struct {
		info *ServerInfo
		want string
	}{
		{&ServerInfo{Build: "abc123"}, "abc123"},
		{&ServerInfo{Build: repoVersion, BuildDate: "2026-01-01"}, repoVersion},
	}
	for _, tt := range tests {
		if got := tt.info.VersionString(); got != tt.want {
			t.Errorf("VersionString() = %q, want %q", got, tt.want)
		}
	}
}

// --- NewEvalHubClient ---

func TestNewEvalHubClientNilWhenNoBaseURL(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{}
	client := NewEvalHubClient(cfg, discardLogger)
	if client != nil {
		t.Error("expected nil client when BaseURL is empty")
	}
}

func TestNewEvalHubClientCreated(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		BaseURL:  "http://localhost:8080",
		Token:    "test-token",
		Tenant:   "test-tenant",
		Insecure: true,
	}
	client := NewEvalHubClient(cfg, discardLogger)
	if client == nil {
		t.Fatal("expected non-nil client when BaseURL is set")
	}
}

// --- MCP server via in-memory transport ---

func TestInitializeHandshake(t *testing.T) {
	t.Parallel()
	connectServer(t, &ServerInfo{Build: "test123"})
}

func TestServerMetadata(t *testing.T) {
	t.Parallel()
	_, cs := connectServer(t, &ServerInfo{Build: "0.2.0"})

	initResult := cs.InitializeResult()
	if initResult == nil {
		t.Fatal("InitializeResult is nil")
	}
	if initResult.ServerInfo.Name != "evalhub-mcp" {
		t.Errorf("server name = %q, want %q", initResult.ServerInfo.Name, "evalhub-mcp")
	}
	if initResult.ServerInfo.Version != "0.2.0" {
		t.Errorf("server version = %q, want %q", initResult.ServerInfo.Version, "0.2.0")
	}
}

func TestCapabilitiesAdvertised(t *testing.T) {
	t.Parallel()
	_, cs := connectServer(t, &ServerInfo{Build: "0.1.0"})

	initResult := cs.InitializeResult()
	if initResult == nil {
		t.Fatal("InitializeResult is nil")
	}
	caps := initResult.Capabilities
	if caps.Tools == nil {
		t.Error("expected tools capability to be advertised")
	}
	if caps.Resources == nil {
		t.Error("expected resources capability to be advertised")
	}
	if caps.Prompts == nil {
		t.Error("expected prompts capability to be advertised")
	}
	if caps.Logging == nil {
		t.Error("expected logging capability to be advertised")
	}
}

func TestToolsListEmpty(t *testing.T) {
	t.Parallel()
	ctx, cs := connectServer(t, &ServerInfo{Build: "0.1.0"})

	toolsResult, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools failed: %v", err)
	}
	if len(toolsResult.Tools) != 0 {
		t.Errorf("expected 0 tools, got %d", len(toolsResult.Tools))
	}
}

func TestResourcesListEmpty(t *testing.T) {
	t.Parallel()
	ctx, cs := connectServer(t, &ServerInfo{Build: "0.1.0"})

	resourcesResult, err := cs.ListResources(ctx, nil)
	if err != nil {
		t.Fatalf("ListResources failed: %v", err)
	}
	if len(resourcesResult.Resources) != 0 {
		t.Errorf("expected 0 resources, got %d", len(resourcesResult.Resources))
	}
}

func TestPromptsListEmpty(t *testing.T) {
	t.Parallel()
	ctx, cs := connectServer(t, &ServerInfo{Build: "0.1.0"})

	promptsResult, err := cs.ListPrompts(ctx, nil)
	if err != nil {
		t.Fatalf("ListPrompts failed: %v", err)
	}
	if len(promptsResult.Prompts) != 0 {
		t.Errorf("expected 0 prompts, got %d", len(promptsResult.Prompts))
	}
}

// --- Transport selection ---

func TestRunHTTPStartsAndStops(t *testing.T) {
	t.Parallel()
	port := freePort(t)

	cfg := &config.Config{
		Transport: config.TransportHTTP,
		Host:      "127.0.0.1",
		Port:      port,
	}
	info := &ServerInfo{Build: "test"}

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- Run(ctx, cfg, info, discardLogger)
	}()

	waitForPort(t, cfg.Host, port, 3*time.Second)

	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("unexpected error after shutdown: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server did not shut down within 5 seconds")
	}
}

func TestRunLegacyHTTPSSEStartsAndStops(t *testing.T) {
	t.Parallel()
	port := freePort(t)

	cfg := &config.Config{
		Transport: config.TransportHTTPSSE,
		Host:      "127.0.0.1",
		Port:      port,
	}
	info := &ServerInfo{Build: "test"}

	ctx, cancel := context.WithCancel(context.Background())

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelWarn}))

	errCh := make(chan error, 1)
	go func() {
		errCh <- Run(ctx, cfg, info, logger)
	}()

	waitForPort(t, cfg.Host, port, 3*time.Second)

	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("unexpected error after shutdown: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server did not shut down within 5 seconds")
	}

	// Read logBuf only after Run returns; slog writes to the buffer from the server goroutine.
	if !strings.Contains(logBuf.String(), "deprecated") {
		t.Errorf("expected deprecation warning in logs, got: %s", logBuf.String())
	}
}

func TestHealthEndpoint(t *testing.T) {
	t.Parallel()
	port := freePort(t)

	cfg := &config.Config{
		Transport: "http",
		Host:      "127.0.0.1",
		Port:      port,
	}
	info := &ServerInfo{Build: "test"}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go Run(ctx, cfg, info, discardLogger) //nolint:errcheck

	waitForPort(t, cfg.Host, port, 3*time.Second)

	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/health", port))
	if err != nil {
		t.Fatalf("health request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("health status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("health content-type = %q, want %q", ct, "application/json")
	}
}

func TestHealthEndpointLegacyHTTPSSE(t *testing.T) {
	t.Parallel()
	port := freePort(t)

	cfg := &config.Config{
		Transport: config.TransportHTTPSSE,
		Host:      "127.0.0.1",
		Port:      port,
	}
	info := &ServerInfo{Build: "test"}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go Run(ctx, cfg, info, discardLogger) //nolint:errcheck

	waitForPort(t, cfg.Host, port, 3*time.Second)

	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/health", port))
	if err != nil {
		t.Fatalf("health request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("health status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestRunHTTPPortInUse(t *testing.T) {
	t.Parallel()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("setting up listener: %v", err)
	}
	defer l.Close()
	port := l.Addr().(*net.TCPAddr).Port

	cfg := &config.Config{
		Transport: "http",
		Host:      "127.0.0.1",
		Port:      port,
	}
	info := &ServerInfo{Build: "test"}

	err = Run(context.Background(), cfg, info, discardLogger)
	if err == nil {
		t.Fatal("expected error when port is in use")
	}
}

func TestRunInvalidTransport(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		Transport: "grpc",
	}
	info := &ServerInfo{Build: "test"}

	err := Run(context.Background(), cfg, info, discardLogger)
	if err == nil {
		t.Fatal("expected error for unsupported transport")
	}
}

// --- helpers ---

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("finding free port: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port
}

func waitForPort(t *testing.T, host string, port int, timeout time.Duration) {
	t.Helper()
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			conn.Close()
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("port %d did not become available within %s", port, timeout)
}
