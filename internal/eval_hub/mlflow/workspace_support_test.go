package mlflow

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/pkg/mlflowclient"
)

// waitForProbeJoiners blocks until wg is done, or fails the test on timeout.
func waitForProbeJoiners(t *testing.T, wg *sync.WaitGroup) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for Resolve callers to join the probe wait")
	}
}

func TestWorkspaceSupport_Resolve(t *testing.T) {
	t.Parallel()

	t.Run("caches enabled result across Apply copies", func(t *testing.T) {
		t.Parallel()
		var probes atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/3.0/mlflow/server-info" {
				http.NotFound(w, r)
				return
			}
			probes.Add(1)
			_ = json.NewEncoder(w).Encode(mlflowclient.ServerInfoResponse{WorkspacesEnabled: true})
		}))
		t.Cleanup(srv.Close)

		support := NewWorkspaceSupport()
		client := mlflowclient.NewClient(srv.URL).WithWorkspace("tenant-a")
		if err := support.Resolve(t.Context(), client); err != nil {
			t.Fatalf("Resolve() = %v", err)
		}
		client = support.Apply(client)
		if !client.WorkspacesEnabled() || !support.Resolved() || !support.Enabled() {
			t.Fatal("expected enabled and resolved")
		}
		if client.WorkspaceName() != "tenant-a" {
			t.Fatalf("workspace name = %q, want tenant-a", client.WorkspaceName())
		}

		copyClient := support.Apply(client.WithContext(t.Context()))
		if err := support.Resolve(t.Context(), copyClient); err != nil {
			t.Fatalf("second Resolve() = %v", err)
		}
		if probes.Load() != 1 {
			t.Fatalf("probes = %d, want 1 (cached)", probes.Load())
		}
		if !copyClient.WorkspacesEnabled() {
			t.Fatal("expected enabled state applied on copy")
		}
	})

	t.Run("leaves support unknown after probe failure", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
		}))
		t.Cleanup(srv.Close)

		support := NewWorkspaceSupport()
		client := mlflowclient.NewClient(srv.URL).WithWorkspace("keep-me")
		err := support.Resolve(t.Context(), client)
		if err == nil {
			t.Fatal("expected probe failure")
		}
		if !errors.Is(err, ErrWorkspaceSupportUnresolved) {
			t.Fatalf("error = %v, want ErrWorkspaceSupportUnresolved", err)
		}
		if support.Resolved() {
			t.Fatal("expected support still unknown")
		}
		if support.Enabled() {
			t.Fatal("expected workspaces not enabled")
		}
		if client.WorkspaceName() != "keep-me" {
			t.Fatalf("workspace name = %q, want keep-me retained", client.WorkspaceName())
		}
	})

	t.Run("PrepareClient resolves then applies before EnsureWorkspace", func(t *testing.T) {
		t.Parallel()
		var createCalls int
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.URL.Path == "/api/3.0/mlflow/server-info":
				_ = json.NewEncoder(w).Encode(mlflowclient.ServerInfoResponse{WorkspacesEnabled: true})
			case r.Method == http.MethodGet && r.URL.Path == "/api/3.0/mlflow/workspaces/lazy-tenant":
				http.Error(w, `{"error_code":"RESOURCE_DOES_NOT_EXIST","message":"not found"}`, http.StatusNotFound)
			case r.Method == http.MethodPost && r.URL.Path == "/api/3.0/mlflow/workspaces":
				createCalls++
				_ = json.NewEncoder(w).Encode(mlflowclient.GetWorkspaceResponse{
					Workspace: mlflowclient.Workspace{Name: "lazy-tenant"},
				})
			default:
				http.NotFound(w, r)
			}
		}))
		t.Cleanup(srv.Close)

		support := NewWorkspaceSupport()
		client := mlflowclient.NewClient(srv.URL).WithWorkspace("lazy-tenant")
		if support.Resolved() {
			t.Fatal("expected unknown support before PrepareClient")
		}
		prepared, err := support.PrepareClient(t.Context(), client)
		if err != nil {
			t.Fatalf("PrepareClient() = %v", err)
		}
		if err := prepared.EnsureWorkspace(); err != nil {
			t.Fatalf("EnsureWorkspace() = %v", err)
		}
		if !prepared.WorkspacesEnabled() || !support.Enabled() {
			t.Fatal("expected workspaces enabled after PrepareClient")
		}
		if createCalls != 1 {
			t.Fatalf("createCalls = %d, want 1", createCalls)
		}
	})

	t.Run("honors already cancelled context", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(200 * time.Millisecond)
			_ = json.NewEncoder(w).Encode(mlflowclient.ServerInfoResponse{WorkspacesEnabled: true})
		}))
		t.Cleanup(srv.Close)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		support := NewWorkspaceSupport()
		client := mlflowclient.NewClient(srv.URL).WithWorkspace("ws")
		err := support.Resolve(ctx, client)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want context.Canceled", err)
		}
		if support.Resolved() {
			t.Fatal("expected support still unknown after cancel")
		}
	})

	t.Run("cancelled context is ignored when support already resolved", func(t *testing.T) {
		t.Parallel()
		support := NewWorkspaceSupport()
		client := mlflowclient.NewClient("http://example")
		// Seed resolved state via Apply path: resolve against a live server first.
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(mlflowclient.ServerInfoResponse{WorkspacesEnabled: true})
		}))
		t.Cleanup(srv.Close)
		if err := support.Resolve(t.Context(), mlflowclient.NewClient(srv.URL)); err != nil {
			t.Fatalf("seed Resolve() = %v", err)
		}

		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := support.Resolve(ctx, client); err != nil {
			t.Fatalf("Resolve() = %v, want nil when already resolved", err)
		}
	})

	t.Run("cancels while waiting for in-flight probe", func(t *testing.T) {
		t.Parallel()
		started := make(chan struct{})
		release := make(chan struct{})
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			close(started)
			<-release
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
		}))
		t.Cleanup(srv.Close)

		support := NewWorkspaceSupport()
		client := mlflowclient.NewClient(srv.URL)
		var joined sync.WaitGroup
		joined.Add(2) // leader + waiter both wait on the shared probe
		support.onJoinWait = func() { joined.Done() }

		leaderDone := make(chan error, 1)
		go func() {
			leaderDone <- support.Resolve(context.Background(), client)
		}()
		<-started

		waiterCtx, cancel := context.WithCancel(context.Background())
		waiterErr := make(chan error, 1)
		go func() {
			waiterErr <- support.Resolve(waiterCtx, client)
		}()
		waitForProbeJoiners(t, &joined)
		support.onJoinWait = nil
		cancel()
		if err := <-waiterErr; !errors.Is(err, context.Canceled) {
			t.Fatalf("waiter error = %v, want context.Canceled", err)
		}
		close(release)
		if err := <-leaderDone; !errors.Is(err, ErrWorkspaceSupportUnresolved) {
			t.Fatalf("leader error = %v, want ErrWorkspaceSupportUnresolved", err)
		}
	})

	t.Run("leader cancellation does not fail active waiter", func(t *testing.T) {
		t.Parallel()
		started := make(chan struct{})
		release := make(chan struct{})
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			close(started)
			<-release
			_ = json.NewEncoder(w).Encode(mlflowclient.ServerInfoResponse{WorkspacesEnabled: true})
		}))
		t.Cleanup(srv.Close)

		support := NewWorkspaceSupport()
		client := mlflowclient.NewClient(srv.URL)
		var joined sync.WaitGroup
		joined.Add(2)
		support.onJoinWait = func() { joined.Done() }

		leaderCtx, leaderCancel := context.WithCancel(context.Background())
		leaderErr := make(chan error, 1)
		go func() {
			leaderErr <- support.Resolve(leaderCtx, client)
		}()
		<-started

		waiterErr := make(chan error, 1)
		go func() {
			waiterErr <- support.Resolve(context.Background(), client)
		}()
		waitForProbeJoiners(t, &joined)
		support.onJoinWait = nil
		leaderCancel()
		if err := <-leaderErr; !errors.Is(err, context.Canceled) {
			t.Fatalf("leader error = %v, want context.Canceled", err)
		}
		close(release)
		if err := <-waiterErr; err != nil {
			t.Fatalf("waiter error = %v, want nil (probe should complete independently)", err)
		}
		if !support.Enabled() {
			t.Fatal("expected shared probe success to enable workspaces for waiter")
		}
	})

	t.Run("concurrent callers share one failed probe", func(t *testing.T) {
		t.Parallel()
		var probes atomic.Int32
		block := make(chan struct{})
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			probes.Add(1)
			<-block
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
		}))
		t.Cleanup(srv.Close)

		support := NewWorkspaceSupport()
		client := mlflowclient.NewClient(srv.URL)
		const n = 8
		var joined sync.WaitGroup
		joined.Add(n)
		support.onJoinWait = func() { joined.Done() }

		errs := make(chan error, n)
		for i := 0; i < n; i++ {
			go func() {
				errs <- support.Resolve(context.Background(), client)
			}()
		}
		// All callers must be waiting on the shared probe before it completes,
		// otherwise a late starter could open a second HTTP probe.
		waitForProbeJoiners(t, &joined)
		support.onJoinWait = nil
		close(block)
		for i := 0; i < n; i++ {
			if err := <-errs; !errors.Is(err, ErrWorkspaceSupportUnresolved) {
				t.Fatalf("error = %v, want ErrWorkspaceSupportUnresolved", err)
			}
		}
		if probes.Load() != 1 {
			t.Fatalf("probes = %d, want 1 shared attempt", probes.Load())
		}
		if err := support.Resolve(context.Background(), client); !errors.Is(err, ErrWorkspaceSupportUnresolved) {
			t.Fatalf("retry error = %v", err)
		}
		if probes.Load() != 2 {
			t.Fatalf("probes after retry = %d, want 2", probes.Load())
		}
	})

	t.Run("resolved capability is not re-probed", func(t *testing.T) {
		t.Parallel()
		var probes atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			probes.Add(1)
			_ = json.NewEncoder(w).Encode(mlflowclient.ServerInfoResponse{WorkspacesEnabled: true})
		}))
		t.Cleanup(srv.Close)

		support := NewWorkspaceSupport()
		client := mlflowclient.NewClient(srv.URL)
		if err := support.Resolve(t.Context(), client); err != nil {
			t.Fatalf("Resolve() = %v", err)
		}
		if err := support.Resolve(t.Context(), client); err != nil {
			t.Fatalf("second Resolve() = %v", err)
		}
		if probes.Load() != 1 {
			t.Fatalf("probes = %d, want 1 (no refresh after success)", probes.Load())
		}
	})

	t.Run("recovery across separate operations after unknown", func(t *testing.T) {
		t.Parallel()
		var probes atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/3.0/mlflow/server-info" {
				http.NotFound(w, r)
				return
			}
			if probes.Add(1) == 1 {
				http.Error(w, "down", http.StatusServiceUnavailable)
				return
			}
			_ = json.NewEncoder(w).Encode(mlflowclient.ServerInfoResponse{WorkspacesEnabled: true})
		}))
		t.Cleanup(srv.Close)

		support := NewWorkspaceSupport()
		client := mlflowclient.NewClient(srv.URL).WithWorkspace("ws")
		if err := support.Resolve(t.Context(), client); err == nil {
			t.Fatal("expected first resolve to fail")
		}
		if support.Resolved() {
			t.Fatal("expected unknown after failure")
		}
		if err := support.Resolve(t.Context(), client); err != nil {
			t.Fatalf("second Resolve() = %v", err)
		}
		if !support.Enabled() {
			t.Fatal("expected enabled after recovery")
		}
	})
}

func TestWorkspaceSupport_edgeCases(t *testing.T) {
	t.Parallel()

	t.Run("nil support", func(t *testing.T) {
		t.Parallel()
		var support *WorkspaceSupport
		if err := support.Resolve(t.Context(), mlflowclient.NewClient("http://example")); err == nil {
			t.Fatal("expected error for nil support")
		}
	})

	t.Run("nil client", func(t *testing.T) {
		t.Parallel()
		if err := NewWorkspaceSupport().Resolve(t.Context(), nil); err == nil {
			t.Fatal("expected error for nil client")
		}
	})

	t.Run("nil context", func(t *testing.T) {
		t.Parallel()
		var ctx context.Context
		if err := NewWorkspaceSupport().Resolve(ctx, mlflowclient.NewClient("http://example")); err == nil {
			t.Fatal("expected error for nil context")
		}
	})

	t.Run("disabled server warns when workspace configured", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(mlflowclient.ServerInfoResponse{WorkspacesEnabled: false})
		}))
		t.Cleanup(srv.Close)

		support := NewWorkspaceSupport()
		client := mlflowclient.NewClient(srv.URL).WithWorkspace("configured-ws")
		if err := support.Resolve(t.Context(), client); err != nil {
			t.Fatalf("Resolve() = %v", err)
		}
		client = support.Apply(client)
		if !support.Resolved() || support.Enabled() || client.WorkspacesEnabled() {
			t.Fatal("expected resolved disabled support")
		}
		if client.WorkspaceName() != "configured-ws" {
			t.Fatalf("WorkspaceName() = %q, want configured-ws", client.WorkspaceName())
		}
	})

	t.Run("Apply leaves client unchanged when unresolved", func(t *testing.T) {
		t.Parallel()
		support := NewWorkspaceSupport()
		client := mlflowclient.NewClient("http://example").WithWorkspacesSupport(true)
		got := support.Apply(client)
		if !got.WorkspacesEnabled() {
			t.Fatal("expected unresolved Apply to leave client enabled flag unchanged")
		}
	})

	t.Run("nil helpers are safe", func(t *testing.T) {
		t.Parallel()
		var support *WorkspaceSupport
		if support.Enabled() || support.Resolved() {
			t.Fatal("nil support should report false")
		}
		client := mlflowclient.NewClient("http://example")
		if support.Apply(client) != client {
			t.Fatal("nil Apply should return client unchanged")
		}
		got, err := support.PrepareClient(t.Context(), client)
		if err != nil || got != client {
			t.Fatalf("nil PrepareClient = (%v, %v)", got != nil, err)
		}
	})
}
