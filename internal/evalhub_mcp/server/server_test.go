package server

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/evalhub_mcp/config"
)

func TestRunHTTPStartsAndStops(t *testing.T) {
	port := freePort(t)

	cfg := &config.Config{
		Transport: "http",
		Host:      "127.0.0.1",
		Port:      port,
	}

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- Run(ctx, cfg, "test")
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

	err = Run(context.Background(), cfg, "test")
	if err == nil {
		t.Fatal("expected error when port is in use")
	}
}

func TestRunInvalidTransport(t *testing.T) {
	cfg := &config.Config{
		Transport: "grpc",
	}
	err := Run(context.Background(), cfg, "test")
	if err == nil {
		t.Fatal("expected error for unsupported transport")
	}
}

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
