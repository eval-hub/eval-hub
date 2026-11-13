package testutil

import (
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"testing"
)

// listenOnFreePort binds an ephemeral port on the default host and returns the
// listener plus the port it took. headroom is the number of ports that must
// remain above it, so port-scan tests always have candidates to fall back to.
func listenOnFreePort(t *testing.T, headroom int) (net.Listener, int) {
	t.Helper()

	for attempt := 0; attempt < 16; attempt++ {
		listener, err := net.Listen("tcp", net.JoinHostPort(defaultHost, "0"))
		if err != nil {
			t.Fatalf("listen on an ephemeral port: %v", err)
		}
		addr, ok := listener.Addr().(*net.TCPAddr)
		if !ok {
			_ = listener.Close()
			t.Fatalf("unexpected listener address type %T", listener.Addr())
		}
		if addr.Port+headroom <= 65535 {
			t.Cleanup(func() { _ = listener.Close() })
			return listener, addr.Port
		}
		_ = listener.Close()
	}
	t.Fatalf("could not bind an ephemeral port with %d ports of headroom", headroom)
	return nil, 0
}

func TestPortAvailable(t *testing.T) {
	t.Run("released port", func(t *testing.T) {
		listener, port := listenOnFreePort(t, 0)
		if err := listener.Close(); err != nil {
			t.Fatalf("close listener on port %d: %v", port, err)
		}
		if !PortAvailable(defaultHost, port) {
			t.Fatalf("PortAvailable(%s, %d) = false for a released port", defaultHost, port)
		}
	})

	t.Run("port held by another listener", func(t *testing.T) {
		_, port := listenOnFreePort(t, 0)
		if PortAvailable(defaultHost, port) {
			t.Fatalf("PortAvailable(%s, %d) = true while the port is bound", defaultHost, port)
		}
	})
}

func TestNextAvailablePort(t *testing.T) {
	t.Run("keeps the assigned port when it is free", func(t *testing.T) {
		listener, port := listenOnFreePort(t, 0)
		if err := listener.Close(); err != nil {
			t.Fatalf("close listener on port %d: %v", port, err)
		}
		got, err := NextAvailablePort(defaultHost, port, 10)
		if err != nil {
			t.Fatalf("NextAvailablePort() err = %v", err)
		}
		if got != port {
			t.Fatalf("NextAvailablePort() = %d, want the assigned port %d", got, port)
		}
	})

	t.Run("tries the next port when the assigned one is taken", func(t *testing.T) {
		_, port := listenOnFreePort(t, 10)
		got, err := NextAvailablePort(defaultHost, port, 10)
		if err != nil {
			t.Fatalf("NextAvailablePort() err = %v", err)
		}
		if got <= port {
			t.Fatalf("NextAvailablePort() = %d, want a port above the busy port %d", got, port)
		}
		if !PortAvailable(defaultHost, got) {
			t.Fatalf("NextAvailablePort() = %d but that port is not available", got)
		}
	})

	t.Run("errors when no candidate in range is free", func(t *testing.T) {
		_, port := listenOnFreePort(t, 0)
		if _, err := NextAvailablePort(defaultHost, port, 1); err == nil {
			t.Fatal("expected an error when the only candidate port is busy")
		}
	})

	t.Run("rejects an invalid start port", func(t *testing.T) {
		for _, start := range []int{-1, 0, 65536} {
			if _, err := NextAvailablePort(defaultHost, start, 1); err == nil {
				t.Fatalf("NextAvailablePort(%d) expected an error", start)
			}
		}
	})
}

func TestServerOptionsWithAvailablePort(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("keeps the assigned port when free", func(t *testing.T) {
		listener, port := listenOnFreePort(t, 0)
		if err := listener.Close(); err != nil {
			t.Fatalf("close listener on port %d: %v", port, err)
		}

		opts := ServerOptions{Host: defaultHost, Port: port, Logger: logger}.
			withConnectionDefaults().
			withAvailablePort(t).
			withStorageDefaults()

		if opts.Port != port {
			t.Fatalf("Port = %d, want the assigned port %d", opts.Port, port)
		}
		if want := fmt.Sprintf("sqlite:///bin/mlflow_test_%d.db", port); opts.BackendStoreURI != want {
			t.Fatalf("BackendStoreURI = %q, want %q", opts.BackendStoreURI, want)
		}
	})

	t.Run("moves to the next port and derives storage paths from it", func(t *testing.T) {
		_, busyPort := listenOnFreePort(t, 10)

		opts := ServerOptions{Host: defaultHost, Port: busyPort, Logger: logger}.
			withConnectionDefaults().
			withAvailablePort(t).
			withStorageDefaults()

		if opts.Port <= busyPort {
			t.Fatalf("Port = %d, want a port above the busy port %d", opts.Port, busyPort)
		}
		if !PortAvailable(defaultHost, opts.Port) {
			t.Fatalf("Port = %d but that port is not available", opts.Port)
		}
		want := fmt.Sprintf("sqlite:///bin/mlflow_test_%d.db", opts.Port)
		if opts.BackendStoreURI != want {
			t.Fatalf("BackendStoreURI = %q, want %q", opts.BackendStoreURI, want)
		}
		for _, path := range []string{opts.ArtifactRoot, opts.ArtifactsDestination} {
			if !strings.Contains(path, fmt.Sprintf("_%d", opts.Port)) {
				t.Fatalf("derived path %q does not use the fallback port %d", path, opts.Port)
			}
		}
	})
}
