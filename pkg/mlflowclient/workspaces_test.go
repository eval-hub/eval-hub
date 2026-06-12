package mlflowclient

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
