package mlflowclient

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

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

		enabled, err := NewClient(srv.URL).ProbeWorkspacesEnabled()
		if err != nil {
			t.Fatalf("ProbeWorkspacesEnabled() = %v", err)
		}
		if enabled {
			t.Fatal("expected workspaces disabled")
		}
	})

	t.Run("missing endpoint is disabled", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		}))
		t.Cleanup(srv.Close)

		enabled, err := NewClient(srv.URL).ProbeWorkspacesEnabled()
		if err != nil {
			t.Fatalf("ProbeWorkspacesEnabled() = %v", err)
		}
		if enabled {
			t.Fatal("expected workspaces disabled for 404")
		}
	})
}

func TestEnsureWorkspace(t *testing.T) {
	t.Parallel()

	t.Run("creates missing workspace", func(t *testing.T) {
		t.Parallel()
		var createCalls int
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == http.MethodGet && r.URL.Path == "/api/3.0/mlflow/workspaces/test-tenant":
				http.Error(w, `{"error_code":"RESOURCE_DOES_NOT_EXIST","message":"not found"}`, http.StatusNotFound)
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
			switch {
			case r.Method == http.MethodGet && r.URL.Path == "/api/3.0/mlflow/workspaces/test-tenant":
				_ = json.NewEncoder(w).Encode(GetWorkspaceResponse{Workspace: Workspace{Name: "test-tenant"}})
			case r.Method == http.MethodPost && r.URL.Path == "/api/3.0/mlflow/workspaces":
				createCalls++
				http.Error(w, "unexpected create", http.StatusInternalServerError)
			default:
				http.NotFound(w, r)
			}
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
	var c *Client
	if c.WorkspacesEnabled() {
		t.Fatal("nil client should report false")
	}
	if !NewClient("http://example").WithWorkspacesSupport(true).WorkspacesEnabled() {
		t.Fatal("expected enabled")
	}
	if NewClient("http://example").WithWorkspacesSupport(false).WorkspacesEnabled() {
		t.Fatal("expected disabled")
	}
}

func TestApplyWorkspaceHeaders(t *testing.T) {
	t.Parallel()
	req, err := http.NewRequest(http.MethodGet, "http://example/x", nil)
	if err != nil {
		t.Fatal(err)
	}
	NewClient("http://example").WithWorkspacesSupport(true).WithWorkspace("ws-a").applyWorkspaceHeaders(req.Header)
	if got := req.Header.Get("X-MLFLOW-WORKSPACE"); got != "ws-a" {
		t.Fatalf("header = %q, want ws-a", got)
	}

	req2, _ := http.NewRequest(http.MethodGet, "http://example/x", nil)
	NewClient("http://example").WithWorkspace("ws-a").applyWorkspaceHeaders(req2.Header)
	if got := req2.Header.Get("X-MLFLOW-WORKSPACE"); got != "" {
		t.Fatalf("header = %q, want empty when support disabled", got)
	}
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

		if _, err := NewClient(srv.URL).WithContext(t.Context()).ProbeWorkspacesEnabled(); err == nil {
			t.Fatal("expected error for 500")
		}
	})
}

func TestEnsureWorkspace_edgeCases(t *testing.T) {
	t.Parallel()

	t.Run("default workspace is no-op", func(t *testing.T) {
		t.Parallel()
		if err := NewClient("http://example").WithWorkspacesSupport(true).WithWorkspace("default").EnsureWorkspace(); err != nil {
			t.Fatalf("EnsureWorkspace() = %v", err)
		}
	})

	t.Run("workspaces disabled is no-op", func(t *testing.T) {
		t.Parallel()
		if err := NewClient("http://example").WithWorkspacesSupport(false).WithWorkspace("tenant").EnsureWorkspace(); err != nil {
			t.Fatalf("EnsureWorkspace() = %v", err)
		}
	})

	t.Run("nil client", func(t *testing.T) {
		t.Parallel()
		var c *Client
		if err := c.EnsureWorkspace(); err == nil {
			t.Fatal("expected error for nil client")
		}
	})

	t.Run("enabled with empty workspace name is no-op", func(t *testing.T) {
		t.Parallel()
		if err := NewClient("http://example").WithWorkspacesSupport(true).EnsureWorkspace(); err != nil {
			t.Fatalf("EnsureWorkspace() = %v", err)
		}
	})
}

func TestGetWorkspace_validation(t *testing.T) {
	t.Parallel()
	var c *Client
	if _, err := c.GetWorkspace("x"); err == nil {
		t.Fatal("expected error for nil client")
	}
	if _, err := NewClient("http://example").GetWorkspace("  "); err == nil {
		t.Fatal("expected error for empty workspace name")
	}
}

func TestCreateWorkspace_validation(t *testing.T) {
	t.Parallel()
	var c *Client
	if _, err := c.CreateWorkspace(&CreateWorkspaceRequest{Name: "x"}); err == nil {
		t.Fatal("expected error for nil client")
	}
	client := NewClient("http://example")
	if _, err := client.CreateWorkspace(nil); err == nil {
		t.Fatal("expected error for nil request")
	}
	if _, err := client.CreateWorkspace(&CreateWorkspaceRequest{Name: "  "}); err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestWithWorkspaceRespectsServerSupport(t *testing.T) {
	t.Parallel()

	t.Run("tenant workspace names are not shared across copies", func(t *testing.T) {
		t.Parallel()
		var gotHeader atomic.Value
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotHeader.Store(r.Header.Get("X-MLFLOW-WORKSPACE"))
			_ = json.NewEncoder(w).Encode(GetExperimentResponse{
				Experiment: Experiment{ExperimentID: "1", Name: "demo", LifecycleStage: "active"},
			})
		}))
		t.Cleanup(srv.Close)

		base := NewClient(srv.URL).WithWorkspacesSupport(true)
		tenantA := base.WithWorkspace("tenant-a")
		_ = base.WithWorkspace("tenant-b") // must not mutate tenant-a

		if tenantA.WorkspaceName() != "tenant-a" {
			t.Fatalf("tenant-a name = %q after sibling WithWorkspace", tenantA.WorkspaceName())
		}
		if _, err := tenantA.GetExperimentByName("demo"); err != nil {
			t.Fatalf("GetExperimentByName() = %v", err)
		}
		if got, _ := gotHeader.Load().(string); got != "tenant-a" {
			t.Fatalf("X-MLFLOW-WORKSPACE = %q, want tenant-a", got)
		}
	})

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
		if got := <-headerCh; got != "" {
			t.Fatalf("X-MLFLOW-WORKSPACE = %q, want empty", got)
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
		if got := <-headerCh; got != "test-tenant" {
			t.Fatalf("X-MLFLOW-WORKSPACE = %q, want test-tenant", got)
		}
	})
}

func TestWorkspaceHelpers_nilClient(t *testing.T) {
	t.Parallel()
	var c *Client
	if c.WorkspacesEnabled() || c.WorkspaceName() != "" || c.configuredWorkspaceName() != "" || c.workspaceHeaderValue() != "" {
		t.Fatal("nil client should report empty/false helpers")
	}
}
