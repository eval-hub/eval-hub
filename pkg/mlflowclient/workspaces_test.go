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

func TestWithWorkspaceRespectsServerSupport(t *testing.T) {
	t.Parallel()

	t.Run("omits header when workspaces disabled", func(t *testing.T) {
		t.Parallel()
		var gotWorkspaceHeader string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotWorkspaceHeader = r.Header.Get("X-MLFLOW-WORKSPACE")
			if r.URL.Path == endpointExperimentsGetByNameBase {
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
		if gotWorkspaceHeader != "" {
			t.Fatalf("X-MLFLOW-WORKSPACE = %q, want empty", gotWorkspaceHeader)
		}
	})

	t.Run("sends header when workspaces enabled", func(t *testing.T) {
		t.Parallel()
		var gotWorkspaceHeader string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotWorkspaceHeader = r.Header.Get("X-MLFLOW-WORKSPACE")
			if r.URL.Path == endpointExperimentsGetByNameBase {
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
		if gotWorkspaceHeader != "test-tenant" {
			t.Fatalf("X-MLFLOW-WORKSPACE = %q, want test-tenant", gotWorkspaceHeader)
		}
	})
}
