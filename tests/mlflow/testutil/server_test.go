package testutil

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

// fakeListener satisfies net.Listener without holding a real socket, so a port
// the fake binder reports as free cannot be claimed by another process before
// the assertion runs.
type fakeListener struct {
	port int
}

func (l *fakeListener) Accept() (net.Conn, error) {
	return nil, errors.New("fake listener does not accept connections")
}

func (l *fakeListener) Close() error { return nil }

func (l *fakeListener) Addr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: l.port}
}

// fakeBinder is a controllable stand-in for net.Listen: the ports in busy fail
// with EADDRINUSE and every other port binds successfully.
type fakeBinder struct {
	busy map[int]bool
}

func (b *fakeBinder) bind(network, address string) (net.Listener, error) {
	port, err := portFromAddress(address)
	if err != nil {
		return nil, err
	}
	if b.busy[port] {
		return nil, &net.OpError{Op: "listen", Net: network, Err: syscall.EADDRINUSE}
	}
	return &fakeListener{port: port}, nil
}

// useFakeBinder replaces the package bind function for the current test and
// restores it afterwards, making port availability deterministic.
func useFakeBinder(t *testing.T, busy ...int) *fakeBinder {
	t.Helper()

	binder := &fakeBinder{busy: make(map[int]bool, len(busy))}
	for _, port := range busy {
		binder.busy[port] = true
	}
	previous := listenTCP
	listenTCP = binder.bind
	t.Cleanup(func() { listenTCP = previous })
	return binder
}

func portFromAddress(address string) (int, error) {
	_, port, err := net.SplitHostPort(address)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(port)
}

// bindEphemeralPort binds an ephemeral port on the default host and keeps the
// listener open for the duration of the test. Holding a real socket makes the
// port unusable for anyone else, so the returned port cannot be claimed before
// an assertion runs. Only port exhaustion (EADDRINUSE) is retried; any other
// bind error is reported unchanged.
func bindEphemeralPort(t *testing.T) (net.Listener, int) {
	t.Helper()

	var lastErr error
	for attempt := 0; attempt < 16; attempt++ {
		listener, err := net.Listen("tcp", net.JoinHostPort(defaultHost, "0"))
		if err != nil {
			if !errors.Is(err, syscall.EADDRINUSE) {
				t.Fatalf("listen on an ephemeral port: %v", err)
			}
			lastErr = err
			continue
		}
		addr, ok := listener.Addr().(*net.TCPAddr)
		if !ok {
			_ = listener.Close()
			t.Fatalf("unexpected listener address type %T", listener.Addr())
		}
		t.Cleanup(func() { _ = listener.Close() })
		return listener, addr.Port
	}
	t.Fatalf("listen on an ephemeral port after repeated EADDRINUSE: %v", lastErr)
	return nil, 0
}

func TestPortAvailable(t *testing.T) {
	t.Run("free port", func(t *testing.T) {
		useFakeBinder(t)

		if !PortAvailable(defaultHost, defaultPort) {
			t.Fatalf("PortAvailable(%s, %d) = false for a free port", defaultHost, defaultPort)
		}
	})

	t.Run("port reported as in use", func(t *testing.T) {
		useFakeBinder(t, defaultPort)

		if PortAvailable(defaultHost, defaultPort) {
			t.Fatalf("PortAvailable(%s, %d) = true while the port is in use", defaultHost, defaultPort)
		}
	})

	t.Run("port held by another listener", func(t *testing.T) {
		_, port := bindEphemeralPort(t)
		if PortAvailable(defaultHost, port) {
			t.Fatalf("PortAvailable(%s, %d) = true while the port is bound", defaultHost, port)
		}
	})
}

func TestNextAvailablePort(t *testing.T) {
	t.Run("keeps the assigned port when it is free", func(t *testing.T) {
		useFakeBinder(t)

		got, err := NextAvailablePort(defaultHost, defaultPort, 10)
		if err != nil {
			t.Fatalf("NextAvailablePort() err = %v", err)
		}
		if got != defaultPort {
			t.Fatalf("NextAvailablePort() = %d, want the assigned port %d", got, defaultPort)
		}
	})

	t.Run("tries the next port when the assigned one is taken", func(t *testing.T) {
		useFakeBinder(t, defaultPort)

		got, err := NextAvailablePort(defaultHost, defaultPort, 10)
		if err != nil {
			t.Fatalf("NextAvailablePort() err = %v", err)
		}
		if want := defaultPort + 1; got != want {
			t.Fatalf("NextAvailablePort() = %d, want the next port %d", got, want)
		}
		if !PortAvailable(defaultHost, got) {
			t.Fatalf("NextAvailablePort() = %d but that port is not available", got)
		}
	})

	t.Run("errors when no candidate in range is free", func(t *testing.T) {
		useFakeBinder(t, defaultPort)

		if _, err := NextAvailablePort(defaultHost, defaultPort, 1); err == nil {
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
		useFakeBinder(t)

		opts := ServerOptions{Host: defaultHost, Port: defaultPort, Logger: logger}.
			withConnectionDefaults().
			withAvailablePort(t).
			withStorageDefaults()

		if opts.Port != defaultPort {
			t.Fatalf("Port = %d, want the assigned port %d", opts.Port, defaultPort)
		}
		if want := fmt.Sprintf("sqlite:///bin/mlflow_test_%d.db", defaultPort); opts.BackendStoreURI != want {
			t.Fatalf("BackendStoreURI = %q, want %q", opts.BackendStoreURI, want)
		}
	})

	t.Run("moves to the next port and derives storage paths from it", func(t *testing.T) {
		useFakeBinder(t, defaultPort)

		opts := ServerOptions{Host: defaultHost, Port: defaultPort, Logger: logger}.
			withConnectionDefaults().
			withAvailablePort(t).
			withStorageDefaults()

		wantPort := defaultPort + 1
		if opts.Port != wantPort {
			t.Fatalf("Port = %d, want the next port %d", opts.Port, wantPort)
		}
		if !PortAvailable(defaultHost, opts.Port) {
			t.Fatalf("Port = %d but that port is not available", opts.Port)
		}
		want := fmt.Sprintf("sqlite:///bin/mlflow_test_%d.db", wantPort)
		if opts.BackendStoreURI != want {
			t.Fatalf("BackendStoreURI = %q, want %q", opts.BackendStoreURI, want)
		}
		for _, path := range []string{opts.ArtifactRoot, opts.ArtifactsDestination} {
			if !strings.Contains(path, fmt.Sprintf("_%d", wantPort)) {
				t.Fatalf("derived path %q does not use the fallback port %d", path, wantPort)
			}
		}
	})
}
