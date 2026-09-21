package mlflowclient

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
		t.Fatal("timed out waiting for ResolveWorkspaceSupport callers to join the probe wait")
	}
}

func TestProbeWorkspacesEnabled(t *testing.T) {
	t.Parallel()

	t.Run("workspaces enabled", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != endpointServerInfo {
				t.Fatalf("path = %s, want %s", r.URL.Path, endpointServerInfo)
			}
			if got := r.Header.Get("X-MLFLOW-WORKSPACE"); got != "" {
				t.Fatalf("server-info must not include X-MLFLOW-WORKSPACE, got %q", got)
			}
			_ = json.NewEncoder(w).Encode(ServerInfoResponse{WorkspacesEnabled: true})
		}))
		t.Cleanup(srv.Close)

		client := NewClient(srv.URL)
		enabled, err := client.ProbeWorkspacesEnabled()
		if err != nil {
			t.Fatalf("ProbeWorkspacesEnabled() = %v", err)
		}
		if !enabled {
			t.Fatal("expected workspaces enabled")
		}
	})

	t.Run("workspaces disabled", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(ServerInfoResponse{WorkspacesEnabled: false})
		}))
		t.Cleanup(srv.Close)

		client := NewClient(srv.URL)
		enabled, err := client.ProbeWorkspacesEnabled()
		if err != nil {
			t.Fatalf("ProbeWorkspacesEnabled() = %v", err)
		}
		if enabled {
			t.Fatal("expected workspaces disabled")
		}
	})

	t.Run("server-info missing on old server", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		}))
		t.Cleanup(srv.Close)

		client := NewClient(srv.URL)
		enabled, err := client.ProbeWorkspacesEnabled()
		if err != nil {
			t.Fatalf("ProbeWorkspacesEnabled() = %v", err)
		}
		if enabled {
			t.Fatal("expected false for 404 server-info")
		}
	})
}

func TestEnsureWorkspace(t *testing.T) {
	t.Parallel()

	t.Run("creates workspace when missing", func(t *testing.T) {
		t.Parallel()
		var createCalls int
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == http.MethodGet && r.URL.Path == "/api/3.0/mlflow/workspaces/test-tenant":
				http.Error(w, `{"error_code":"RESOURCE_DOES_NOT_EXIST","message":"Workspace 'test-tenant' not found"}`, http.StatusNotFound)
			case r.Method == http.MethodPost && r.URL.Path == "/api/3.0/mlflow/workspaces":
				createCalls++
				_ = json.NewEncoder(w).Encode(GetWorkspaceResponse{Workspace: Workspace{Name: "test-tenant"}})
			default:
				http.NotFound(w, r)
			}
		}))
		t.Cleanup(srv.Close)

		client := NewClient(srv.URL).WithWorkspacesSupport(true).WithWorkspace("test-tenant")
		if err := client.EnsureWorkspace(); err != nil {
			t.Fatalf("EnsureWorkspace() = %v", err)
		}
		if createCalls != 1 {
			t.Fatalf("createCalls = %d, want 1", createCalls)
		}
	})

	t.Run("skips create when workspace exists", func(t *testing.T) {
		t.Parallel()
		var createCalls int
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet && r.URL.Path == "/api/3.0/mlflow/workspaces/test-tenant" {
				_ = json.NewEncoder(w).Encode(GetWorkspaceResponse{Workspace: Workspace{Name: "test-tenant"}})
				return
			}
			if r.Method == http.MethodPost && r.URL.Path == "/api/3.0/mlflow/workspaces" {
				createCalls++
			}
			http.NotFound(w, r)
		}))
		t.Cleanup(srv.Close)

		client := NewClient(srv.URL).WithWorkspacesSupport(true).WithWorkspace("test-tenant")
		if err := client.EnsureWorkspace(); err != nil {
			t.Fatalf("EnsureWorkspace() = %v", err)
		}
		if createCalls != 0 {
			t.Fatalf("createCalls = %d, want 0", createCalls)
		}
	})
}

func TestWorkspacesEnabled(t *testing.T) {
	t.Parallel()
	if (*Client)(nil).WorkspacesEnabled() {
		t.Fatal("nil client should report false")
	}
	if NewClient("http://example").WorkspacesEnabled() {
		t.Fatal("expected false by default")
	}
	if !NewClient("http://example").WithWorkspacesSupport(true).WorkspacesEnabled() {
		t.Fatal("expected true when enabled")
	}
	if NewClient("http://example").WorkspaceSupportResolved() {
		t.Fatal("expected unresolved support by default")
	}
	if !NewClient("http://example").WithWorkspacesSupport(false).WorkspaceSupportResolved() {
		t.Fatal("expected resolved after WithWorkspacesSupport")
	}
}

func TestResolveWorkspaceSupport(t *testing.T) {
	t.Parallel()

	t.Run("caches enabled result across WithContext copies", func(t *testing.T) {
		t.Parallel()
		var probes atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != endpointServerInfo {
				http.NotFound(w, r)
				return
			}
			probes.Add(1)
			_ = json.NewEncoder(w).Encode(ServerInfoResponse{WorkspacesEnabled: true})
		}))
		t.Cleanup(srv.Close)

		client := NewClient(srv.URL).WithWorkspace("tenant-a")
		if err := client.ResolveWorkspaceSupport(t.Context()); err != nil {
			t.Fatalf("ResolveWorkspaceSupport() = %v", err)
		}
		if !client.WorkspacesEnabled() || !client.WorkspaceSupportResolved() {
			t.Fatal("expected enabled and resolved")
		}
		if client.configuredWorkspaceName() != "tenant-a" {
			t.Fatalf("workspace name = %q, want tenant-a", client.configuredWorkspaceName())
		}

		copyClient := client.WithContext(t.Context())
		if err := copyClient.ResolveWorkspaceSupport(t.Context()); err != nil {
			t.Fatalf("second ResolveWorkspaceSupport() = %v", err)
		}
		if probes.Load() != 1 {
			t.Fatalf("probes = %d, want 1 (cached)", probes.Load())
		}
		if !copyClient.WorkspacesEnabled() {
			t.Fatal("expected shared enabled state on copy")
		}
	})

	t.Run("leaves support unknown after probe failure", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
		}))
		t.Cleanup(srv.Close)

		client := NewClient(srv.URL).WithWorkspace("keep-me")
		err := client.ResolveWorkspaceSupport(t.Context())
		if err == nil {
			t.Fatal("expected probe failure")
		}
		if !errors.Is(err, ErrWorkspaceSupportUnresolved) {
			t.Fatalf("error = %v, want ErrWorkspaceSupportUnresolved", err)
		}
		if client.WorkspaceSupportResolved() {
			t.Fatal("expected support still unknown")
		}
		if client.WorkspacesEnabled() {
			t.Fatal("expected workspaces not enabled")
		}
		if client.configuredWorkspaceName() != "keep-me" {
			t.Fatalf("workspace name = %q, want keep-me retained", client.configuredWorkspaceName())
		}
	})

	t.Run("EnsureWorkspace re-probes when unknown then creates workspace", func(t *testing.T) {
		t.Parallel()
		var createCalls int
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.URL.Path == endpointServerInfo:
				_ = json.NewEncoder(w).Encode(ServerInfoResponse{WorkspacesEnabled: true})
			case r.Method == http.MethodGet && r.URL.Path == "/api/3.0/mlflow/workspaces/lazy-tenant":
				http.Error(w, `{"error_code":"RESOURCE_DOES_NOT_EXIST","message":"not found"}`, http.StatusNotFound)
			case r.Method == http.MethodPost && r.URL.Path == "/api/3.0/mlflow/workspaces":
				createCalls++
				_ = json.NewEncoder(w).Encode(GetWorkspaceResponse{Workspace: Workspace{Name: "lazy-tenant"}})
			default:
				http.NotFound(w, r)
			}
		}))
		t.Cleanup(srv.Close)

		client := NewClient(srv.URL).WithWorkspace("lazy-tenant")
		if client.WorkspaceSupportResolved() {
			t.Fatal("expected unknown support before EnsureWorkspace")
		}
		if err := client.EnsureWorkspace(); err != nil {
			t.Fatalf("EnsureWorkspace() = %v", err)
		}
		if !client.WorkspacesEnabled() {
			t.Fatal("expected workspaces enabled after lazy probe")
		}
		if createCalls != 1 {
			t.Fatalf("createCalls = %d, want 1", createCalls)
		}
	})

	t.Run("tenant workspace names are not shared across copies", func(t *testing.T) {
		t.Parallel()
		var gotHeader atomic.Value
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == endpointServerInfo {
				_ = json.NewEncoder(w).Encode(ServerInfoResponse{WorkspacesEnabled: true})
				return
			}
			gotHeader.Store(r.Header.Get("X-MLFLOW-WORKSPACE"))
			_ = json.NewEncoder(w).Encode(GetExperimentResponse{
				Experiment: Experiment{ExperimentID: "1", Name: "demo", LifecycleStage: "active"},
			})
		}))
		t.Cleanup(srv.Close)

		base := NewClient(srv.URL).WithWorkspacesSupport(true)
		tenantA := base.WithWorkspace("tenant-a")
		_ = base.WithWorkspace("tenant-b") // must not mutate tenant-a

		if tenantA.configuredWorkspaceName() != "tenant-a" {
			t.Fatalf("tenant-a name = %q after sibling WithWorkspace", tenantA.configuredWorkspaceName())
		}
		if _, err := tenantA.GetExperimentByName("demo"); err != nil {
			t.Fatalf("GetExperimentByName() = %v", err)
		}
		if got, _ := gotHeader.Load().(string); got != "tenant-a" {
			t.Fatalf("X-MLFLOW-WORKSPACE = %q, want tenant-a", got)
		}
	})

	t.Run("honors already cancelled context", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(200 * time.Millisecond)
			_ = json.NewEncoder(w).Encode(ServerInfoResponse{WorkspacesEnabled: true})
		}))
		t.Cleanup(srv.Close)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		client := NewClient(srv.URL).WithWorkspace("ws")
		err := client.ResolveWorkspaceSupport(ctx)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want context.Canceled", err)
		}
		if client.WorkspaceSupportResolved() {
			t.Fatal("expected support still unknown after cancel")
		}
	})

	t.Run("cancelled context is ignored when support already resolved", func(t *testing.T) {
		t.Parallel()
		client := NewClient("http://example").WithWorkspacesSupport(true)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := client.ResolveWorkspaceSupport(ctx); err != nil {
			t.Fatalf("ResolveWorkspaceSupport() = %v, want nil when already resolved", err)
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

		base := NewClient(srv.URL)
		var joined sync.WaitGroup
		joined.Add(2) // leader + waiter both wait on the shared probe
		base.ws.onJoinWait = func() { joined.Done() }

		leaderDone := make(chan error, 1)
		go func() {
			leaderDone <- base.ResolveWorkspaceSupport(context.Background())
		}()
		<-started

		waiterCtx, cancel := context.WithCancel(context.Background())
		waiterErr := make(chan error, 1)
		go func() {
			waiterErr <- base.ResolveWorkspaceSupport(waiterCtx)
		}()
		waitForProbeJoiners(t, &joined)
		base.ws.onJoinWait = nil
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
			_ = json.NewEncoder(w).Encode(ServerInfoResponse{WorkspacesEnabled: true})
		}))
		t.Cleanup(srv.Close)

		base := NewClient(srv.URL)
		var joined sync.WaitGroup
		joined.Add(2)
		base.ws.onJoinWait = func() { joined.Done() }

		leaderCtx, leaderCancel := context.WithCancel(context.Background())
		leaderErr := make(chan error, 1)
		go func() {
			leaderErr <- base.ResolveWorkspaceSupport(leaderCtx)
		}()
		<-started

		waiterErr := make(chan error, 1)
		go func() {
			waiterErr <- base.ResolveWorkspaceSupport(context.Background())
		}()
		waitForProbeJoiners(t, &joined)
		base.ws.onJoinWait = nil
		leaderCancel()
		if err := <-leaderErr; !errors.Is(err, context.Canceled) {
			t.Fatalf("leader error = %v, want context.Canceled", err)
		}
		close(release)
		if err := <-waiterErr; err != nil {
			t.Fatalf("waiter error = %v, want nil (probe should complete independently)", err)
		}
		if !base.WorkspacesEnabled() {
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

		base := NewClient(srv.URL)
		const n = 8
		var joined sync.WaitGroup
		joined.Add(n)
		base.ws.onJoinWait = func() { joined.Done() }

		errs := make(chan error, n)
		for i := 0; i < n; i++ {
			go func() {
				errs <- base.ResolveWorkspaceSupport(context.Background())
			}()
		}
		// All callers must be waiting on the shared probe before it completes,
		// otherwise a late starter could open a second HTTP probe.
		waitForProbeJoiners(t, &joined)
		base.ws.onJoinWait = nil
		close(block)
		for i := 0; i < n; i++ {
			if err := <-errs; !errors.Is(err, ErrWorkspaceSupportUnresolved) {
				t.Fatalf("error = %v, want ErrWorkspaceSupportUnresolved", err)
			}
		}
		if probes.Load() != 1 {
			t.Fatalf("probes = %d, want 1 shared attempt", probes.Load())
		}
		if err := base.ResolveWorkspaceSupport(context.Background()); !errors.Is(err, ErrWorkspaceSupportUnresolved) {
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
			_ = json.NewEncoder(w).Encode(ServerInfoResponse{WorkspacesEnabled: true})
		}))
		t.Cleanup(srv.Close)

		client := NewClient(srv.URL)
		if err := client.ResolveWorkspaceSupport(t.Context()); err != nil {
			t.Fatalf("ResolveWorkspaceSupport() = %v", err)
		}
		if err := client.ResolveWorkspaceSupport(t.Context()); err != nil {
			t.Fatalf("second ResolveWorkspaceSupport() = %v", err)
		}
		if probes.Load() != 1 {
			t.Fatalf("probes = %d, want 1 (no refresh after success)", probes.Load())
		}
	})

	t.Run("recovery across separate operations after unknown", func(t *testing.T) {
		t.Parallel()
		var probes atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != endpointServerInfo {
				http.NotFound(w, r)
				return
			}
			if probes.Add(1) == 1 {
				http.Error(w, "down", http.StatusServiceUnavailable)
				return
			}
			_ = json.NewEncoder(w).Encode(ServerInfoResponse{WorkspacesEnabled: true})
		}))
		t.Cleanup(srv.Close)

		client := NewClient(srv.URL).WithWorkspace("ws")
		if err := client.ResolveWorkspaceSupport(t.Context()); err == nil {
			t.Fatal("expected first resolve to fail")
		}
		if client.WorkspaceSupportResolved() {
			t.Fatal("expected unknown after failure")
		}
		if err := client.ResolveWorkspaceSupport(t.Context()); err != nil {
			t.Fatalf("second ResolveWorkspaceSupport() = %v", err)
		}
		if !client.WorkspacesEnabled() {
			t.Fatal("expected enabled after recovery")
		}
	})
}

func TestProbeWorkspacesEnabled_errors(t *testing.T) {
	t.Parallel()

	t.Run("nil client", func(t *testing.T) {
		t.Parallel()
		var c *Client
		if _, err := c.ProbeWorkspacesEnabled(); err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("nil context", func(t *testing.T) {
		t.Parallel()
		c := NewClient("http://example")
		c.ctx = nil
		if _, err := c.ProbeWorkspacesEnabled(); err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("unexpected status", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "server error", http.StatusInternalServerError)
		}))
		t.Cleanup(srv.Close)

		client := NewClient(srv.URL).WithContext(t.Context())
		if _, err := client.ProbeWorkspacesEnabled(); err == nil {
			t.Fatal("expected error for 500")
		}
	})

	t.Run("invalid JSON body", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("not-json"))
		}))
		t.Cleanup(srv.Close)

		client := NewClient(srv.URL).WithContext(t.Context())
		if _, err := client.ProbeWorkspacesEnabled(); err == nil {
			t.Fatal("expected error for invalid JSON")
		}
	})
}

func TestEnsureWorkspace_edgeCases(t *testing.T) {
	t.Parallel()

	t.Run("default workspace is no-op", func(t *testing.T) {
		t.Parallel()
		client := NewClient("http://example").WithWorkspacesSupport(true).WithWorkspace("default")
		if err := client.EnsureWorkspace(); err != nil {
			t.Fatalf("EnsureWorkspace() = %v", err)
		}
	})

	t.Run("workspaces disabled is no-op", func(t *testing.T) {
		t.Parallel()
		client := NewClient("http://example").WithWorkspacesSupport(false).WithWorkspace("tenant")
		if err := client.EnsureWorkspace(); err != nil {
			t.Fatalf("EnsureWorkspace() = %v", err)
		}
	})

	t.Run("create races with RESOURCE_ALREADY_EXISTS", func(t *testing.T) {
		t.Parallel()
		var getCalls int
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == http.MethodGet && r.URL.Path == "/api/3.0/mlflow/workspaces/race-ws":
				getCalls++
				if getCalls == 1 {
					http.Error(w, `{"error_code":"RESOURCE_DOES_NOT_EXIST"}`, http.StatusNotFound)
					return
				}
				_ = json.NewEncoder(w).Encode(GetWorkspaceResponse{Workspace: Workspace{Name: "race-ws"}})
			case r.Method == http.MethodPost && r.URL.Path == "/api/3.0/mlflow/workspaces":
				http.Error(w, `{"error_code":"RESOURCE_ALREADY_EXISTS"}`, http.StatusBadRequest)
			default:
				http.NotFound(w, r)
			}
		}))
		t.Cleanup(srv.Close)

		client := NewClient(srv.URL).WithContext(t.Context()).WithWorkspacesSupport(true).WithWorkspace("race-ws")
		if err := client.EnsureWorkspace(); err != nil {
			t.Fatalf("EnsureWorkspace() = %v", err)
		}
		if getCalls != 2 {
			t.Fatalf("getCalls = %d, want 2", getCalls)
		}
	})
}

func TestWorkspaceMgmtAPIsIncludeHeader(t *testing.T) {
	t.Parallel()

	t.Run("GetWorkspace sends header when workspaces enabled", func(t *testing.T) {
		t.Parallel()
		headerCh := make(chan string, 1)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			headerCh <- r.Header.Get("X-MLFLOW-WORKSPACE")
			_ = json.NewEncoder(w).Encode(GetWorkspaceResponse{Workspace: Workspace{Name: "my-ws"}})
		}))
		t.Cleanup(srv.Close)

		client := NewClient(srv.URL).WithWorkspacesSupport(true).WithWorkspace("my-ws")
		_, err := client.GetWorkspace("my-ws")
		if err != nil {
			t.Fatalf("GetWorkspace() = %v", err)
		}
		if got := <-headerCh; got != "my-ws" {
			t.Fatalf("X-MLFLOW-WORKSPACE = %q, want %q", got, "my-ws")
		}
	})

	t.Run("GetWorkspace omits header when workspaces disabled", func(t *testing.T) {
		t.Parallel()
		headerCh := make(chan string, 1)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			headerCh <- r.Header.Get("X-MLFLOW-WORKSPACE")
			_ = json.NewEncoder(w).Encode(GetWorkspaceResponse{Workspace: Workspace{Name: "my-ws"}})
		}))
		t.Cleanup(srv.Close)

		client := NewClient(srv.URL).WithWorkspacesSupport(false)
		_, err := client.GetWorkspace("my-ws")
		if err != nil {
			t.Fatalf("GetWorkspace() = %v", err)
		}
		if got := <-headerCh; got != "" {
			t.Fatalf("X-MLFLOW-WORKSPACE = %q, want empty", got)
		}
	})

	t.Run("CreateWorkspace omits header even when workspaces enabled", func(t *testing.T) {
		t.Parallel()
		headerCh := make(chan string, 1)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			headerCh <- r.Header.Get("X-MLFLOW-WORKSPACE")
			_ = json.NewEncoder(w).Encode(GetWorkspaceResponse{Workspace: Workspace{Name: "new-ws"}})
		}))
		t.Cleanup(srv.Close)

		client := NewClient(srv.URL).WithWorkspacesSupport(true).WithWorkspace("my-ws")
		_, err := client.CreateWorkspace(&CreateWorkspaceRequest{Name: "new-ws"})
		if err != nil {
			t.Fatalf("CreateWorkspace() = %v", err)
		}
		if got := <-headerCh; got != "" {
			t.Fatalf("X-MLFLOW-WORKSPACE = %q, want empty", got)
		}
	})
}

func TestGetWorkspace_validation(t *testing.T) {
	t.Parallel()
	var c *Client
	if _, err := c.GetWorkspace("x"); err == nil {
		t.Fatal("expected error for nil client")
	}
	client := NewClient("http://example")
	if _, err := client.GetWorkspace("  "); err == nil {
		t.Fatal("expected error for empty workspace name")
	}
}

func TestWithWorkspaceRespectsServerSupport(t *testing.T) {
	t.Parallel()

	t.Run("omits header when workspaces disabled", func(t *testing.T) {
		t.Parallel()
		headerCh := make(chan string, 1)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == endpointExperimentsGetByNameBase {
				headerCh <- r.Header.Get("X-MLFLOW-WORKSPACE")
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"error_code":"RESOURCE_DOES_NOT_EXIST"}`))
				return
			}
			http.NotFound(w, r)
		}))
		t.Cleanup(srv.Close)

		client := NewClient(srv.URL).WithWorkspacesSupport(false).WithWorkspace("test-tenant")
		_, err := client.GetExperimentByName("demo")
		if err == nil {
			t.Fatal("expected error for missing experiment")
		}
		gotWorkspaceHeader := <-headerCh
		if gotWorkspaceHeader != "" {
			t.Fatalf("X-MLFLOW-WORKSPACE = %q, want empty", gotWorkspaceHeader)
		}
	})

	t.Run("sends header when workspaces enabled", func(t *testing.T) {
		t.Parallel()
		headerCh := make(chan string, 1)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == endpointExperimentsGetByNameBase {
				headerCh <- r.Header.Get("X-MLFLOW-WORKSPACE")
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"error_code":"RESOURCE_DOES_NOT_EXIST"}`))
				return
			}
			http.NotFound(w, r)
		}))
		t.Cleanup(srv.Close)

		client := NewClient(srv.URL).WithWorkspacesSupport(true).WithWorkspace("test-tenant")
		_, err := client.GetExperimentByName("demo")
		if err == nil {
			t.Fatal("expected error for missing experiment")
		}
		gotWorkspaceHeader := <-headerCh
		if gotWorkspaceHeader != "test-tenant" {
			t.Fatalf("X-MLFLOW-WORKSPACE = %q, want test-tenant", gotWorkspaceHeader)
		}
	})
}
