package server

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/evalhub_mcp/config"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var discardLogger = slog.New(slog.DiscardHandler)

// --- ServerInfo ---

func TestVersionString(t *testing.T) {
	t.Parallel()
	tests := []struct {
		info *ServerInfo
		want string
	}{
		{&ServerInfo{Version: "0.1.0"}, "0.1.0"},
		{&ServerInfo{Version: "0.1.0", Build: "abc123"}, "0.1.0+abc123"},
		{&ServerInfo{Version: "0.4.0", Build: "deadbeef", BuildDate: "2026-01-01"}, "0.4.0+deadbeef"},
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
	info := &ServerInfo{Version: "0.1.0", Build: "test123"}
	srv := New(info, discardLogger, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	serverTransport, clientTransport := mcp.NewInMemoryTransports()

	serverSession, err := srv.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect failed: %v", err)
	}
	defer serverSession.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.0.1"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect failed: %v", err)
	}
	defer clientSession.Close()
}

func TestServerMetadata(t *testing.T) {
	t.Parallel()
	info := &ServerInfo{Version: "0.2.0", Build: "deadbeef"}
	srv := New(info, discardLogger, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	serverTransport, clientTransport := mcp.NewInMemoryTransports()

	serverSession, err := srv.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect failed: %v", err)
	}
	defer serverSession.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.0.1"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect failed: %v", err)
	}
	defer clientSession.Close()

	initResult := clientSession.InitializeResult()
	if initResult == nil {
		t.Fatal("InitializeResult is nil")
	}
	if initResult.ServerInfo.Name != "evalhub-mcp" {
		t.Errorf("server name = %q, want %q", initResult.ServerInfo.Name, "evalhub-mcp")
	}
	if initResult.ServerInfo.Version != "0.2.0+deadbeef" {
		t.Errorf("server version = %q, want %q", initResult.ServerInfo.Version, "0.2.0+deadbeef")
	}
}

func TestCapabilitiesAdvertised(t *testing.T) {
	t.Parallel()
	info := &ServerInfo{Version: "0.1.0"}
	srv := New(info, discardLogger, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	serverTransport, clientTransport := mcp.NewInMemoryTransports()

	serverSession, err := srv.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect failed: %v", err)
	}
	defer serverSession.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.0.1"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect failed: %v", err)
	}
	defer clientSession.Close()

	initResult := clientSession.InitializeResult()
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
	info := &ServerInfo{Version: "0.1.0"}
	srv := New(info, discardLogger, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	serverTransport, clientTransport := mcp.NewInMemoryTransports()

	serverSession, err := srv.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect failed: %v", err)
	}
	defer serverSession.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.0.1"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect failed: %v", err)
	}
	defer clientSession.Close()

	toolsResult, err := clientSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools failed: %v", err)
	}
	if len(toolsResult.Tools) != 0 {
		t.Errorf("expected 0 tools, got %d", len(toolsResult.Tools))
	}
}

func TestResourcesListEmpty(t *testing.T) {
	t.Parallel()
	info := &ServerInfo{Version: "0.1.0"}
	srv := New(info, discardLogger, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	serverTransport, clientTransport := mcp.NewInMemoryTransports()

	serverSession, err := srv.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect failed: %v", err)
	}
	defer serverSession.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.0.1"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect failed: %v", err)
	}
	defer clientSession.Close()

	resourcesResult, err := clientSession.ListResources(ctx, nil)
	if err != nil {
		t.Fatalf("ListResources failed: %v", err)
	}
	if len(resourcesResult.Resources) != 0 {
		t.Errorf("expected 0 resources, got %d", len(resourcesResult.Resources))
	}
}

func TestPromptsListEmpty(t *testing.T) {
	t.Parallel()
	info := &ServerInfo{Version: "0.1.0"}
	srv := New(info, discardLogger, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	serverTransport, clientTransport := mcp.NewInMemoryTransports()

	serverSession, err := srv.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect failed: %v", err)
	}
	defer serverSession.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.0.1"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect failed: %v", err)
	}
	defer clientSession.Close()

	promptsResult, err := clientSession.ListPrompts(ctx, nil)
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
		Transport: "http",
		Host:      "127.0.0.1",
		Port:      port,
	}
	info := &ServerInfo{Version: "test"}

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
	info := &ServerInfo{Version: "test"}

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
	info := &ServerInfo{Version: "test"}

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
