package mlflowclient

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// MLflow REST wire paths issued by mlflow-go for workspace and experiment lookups.
const (
	endpointServerInfo               = "/api/3.0/mlflow/server-info"
	endpointExperimentsGetByNameBase = "/api/2.0/mlflow/experiments/get-by-name"
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
		// Get-first semantics: an existing workspace is confirmed with a GET and
		// no create is attempted.
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

func TestEnsureWorkspaceProviderErrors(t *testing.T) {
	t.Parallel()

	// RHAI-17156: the workspace lookup reports the workspace as missing, and the
	// provider answers the create with HTTP 403 and a body outside MLflow's
	// error_code/message schema. The provider's reason has to reach the caller, and
	// the client must not retry the create or re-classify the failure.
	t.Run("forbidden create keeps the provider detail", func(t *testing.T) {
		t.Parallel()
		const notFoundMessage = "Workspace 'new-project' not found in the Kubernetes cluster."
		const creationMessage = "Namespace creation is not supported by this provider"
		var mu sync.Mutex
		var calls []string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			calls = append(calls, r.Method+" "+r.URL.Path)
			mu.Unlock()
			switch {
			case r.Method == http.MethodGet && r.URL.Path == "/api/3.0/mlflow/workspaces/new-project":
				w.WriteHeader(http.StatusNotFound)
				_, _ = fmt.Fprintf(w, `{"error_code":"RESOURCE_DOES_NOT_EXIST","message":%q}`, notFoundMessage)
			case r.Method == http.MethodPost && r.URL.Path == "/api/3.0/mlflow/workspaces":
				w.WriteHeader(http.StatusForbidden)
				_, _ = fmt.Fprintf(w, `{"detail":%q}`, creationMessage)
			default:
				t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				w.WriteHeader(http.StatusInternalServerError)
			}
		}))
		t.Cleanup(srv.Close)

		client := NewClient(srv.URL).WithWorkspacesSupport(true).WithWorkspace("new-project")
		err := client.EnsureWorkspace()
		if err == nil {
			t.Fatal("expected EnsureWorkspace to fail")
		}
		var apiErr *APIError
		if !errors.As(err, &apiErr) {
			t.Fatalf("EnsureWorkspace() = %T %v, want *APIError", err, err)
		}
		if apiErr.StatusCode != http.StatusForbidden {
			t.Fatalf("StatusCode = %d, want %d", apiErr.StatusCode, http.StatusForbidden)
		}
		if apiErr.MLFlowError == nil || apiErr.MLFlowError.Message == "" {
			t.Fatalf("MLFlowError = %+v, want the provider detail preserved", apiErr.MLFlowError)
		}
		if !strings.Contains(err.Error(), creationMessage) {
			t.Fatalf("Error() = %q, want it to contain %q", err.Error(), creationMessage)
		}
		if IsResourceAlreadyExistsError(err) || IsResourceDoesNotExistError(err) {
			t.Fatalf("403 must not be classified as already-exists or not-found: %v", err)
		}

		mu.Lock()
		defer mu.Unlock()
		want := []string{
			"GET /api/3.0/mlflow/workspaces/new-project",
			"POST /api/3.0/mlflow/workspaces",
		}
		if len(calls) != len(want) || calls[0] != want[0] || calls[1] != want[1] {
			t.Fatalf("calls = %v, want %v (no retries or fallback after 403)", calls, want)
		}
	})

	// A concurrent creator can win the race, and the provider may report it with
	// HTTP 409 instead of MLflow's 400 + RESOURCE_ALREADY_EXISTS.
	t.Run("conflict create recovers with a second lookup", func(t *testing.T) {
		t.Parallel()
		var gets atomic.Int32
		var posts atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == http.MethodGet && r.URL.Path == "/api/3.0/mlflow/workspaces/new-project":
				if gets.Add(1) == 1 {
					w.WriteHeader(http.StatusNotFound)
					_, _ = fmt.Fprint(w, `{"error_code":"RESOURCE_DOES_NOT_EXIST","message":"not found"}`)
					return
				}
				_ = json.NewEncoder(w).Encode(GetWorkspaceResponse{Workspace: Workspace{Name: "new-project"}})
			case r.Method == http.MethodPost && r.URL.Path == "/api/3.0/mlflow/workspaces":
				posts.Add(1)
				w.WriteHeader(http.StatusConflict)
				_, _ = fmt.Fprint(w, `{"error_code":"RESOURCE_ALREADY_EXISTS","message":"workspace exists"}`)
			default:
				t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			}
		}))
		t.Cleanup(srv.Close)

		client := NewClient(srv.URL).WithWorkspacesSupport(true).WithWorkspace("new-project")
		if err := client.EnsureWorkspace(); err != nil {
			t.Fatalf("EnsureWorkspace() = %v, want recovery after the conflict", err)
		}
		if posts.Load() != 1 {
			t.Fatalf("create calls = %d, want 1", posts.Load())
		}
		if gets.Load() != 2 {
			t.Fatalf("lookup calls = %d, want 2 (initial miss + confirmation)", gets.Load())
		}
	})

	// If the confirming lookup fails there is no evidence the workspace exists,
	// and the create error is the only one carrying the provider's details.
	t.Run("conflict without a confirming lookup keeps the create error", func(t *testing.T) {
		t.Parallel()
		const conflictMessage = "workspace new-project exists in the provider"
		var gets atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == http.MethodGet && r.URL.Path == "/api/3.0/mlflow/workspaces/new-project":
				if gets.Add(1) == 1 {
					w.WriteHeader(http.StatusNotFound)
					_, _ = fmt.Fprint(w, `{"error_code":"RESOURCE_DOES_NOT_EXIST","message":"not found"}`)
					return
				}
				w.WriteHeader(http.StatusForbidden)
				_, _ = fmt.Fprint(w, `{"error_code":"PERMISSION_DENIED","message":"lookup denied"}`)
			case r.Method == http.MethodPost && r.URL.Path == "/api/3.0/mlflow/workspaces":
				w.WriteHeader(http.StatusConflict)
				_, _ = fmt.Fprintf(w, `{"error_code":"RESOURCE_ALREADY_EXISTS","message":%q}`, conflictMessage)
			default:
				t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			}
		}))
		t.Cleanup(srv.Close)

		client := NewClient(srv.URL).WithWorkspacesSupport(true).WithWorkspace("new-project")
		err := client.EnsureWorkspace()
		if err == nil {
			t.Fatal("expected EnsureWorkspace to fail")
		}
		var apiErr *APIError
		if !errors.As(err, &apiErr) {
			t.Fatalf("EnsureWorkspace() = %T %v, want *APIError from the create", err, err)
		}
		if apiErr.StatusCode != http.StatusConflict {
			t.Fatalf("StatusCode = %d, want %d (the create status, not the lookup status)", apiErr.StatusCode, http.StatusConflict)
		}
		if apiErr.MLFlowError == nil || apiErr.MLFlowError.ErrorCode != "RESOURCE_ALREADY_EXISTS" {
			t.Fatalf("MLFlowError = %+v, want the create error code preserved", apiErr.MLFlowError)
		}
		if !strings.Contains(err.Error(), conflictMessage) {
			t.Fatalf("Error() = %q, want it to contain %q", err.Error(), conflictMessage)
		}
		if !IsResourceAlreadyExistsError(err) {
			t.Fatal("expected the returned error to stay classified as already-exists")
		}
		if gets.Load() != 2 {
			t.Fatalf("lookup calls = %d, want 2 (initial miss + confirmation)", gets.Load())
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

func TestCreateWorkspaceErrorResponse(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/3.0/mlflow/workspaces" {
			http.Error(w, `{"error_code":"INVALID_PARAMETER_VALUE","message":"bad name"}`, http.StatusBadRequest)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)

	client := NewClient(srv.URL).WithContext(t.Context())
	if _, err := client.CreateWorkspace(&CreateWorkspaceRequest{Name: "tenant"}); err == nil {
		t.Fatal("expected CreateWorkspace error")
	}
}

func TestGetWorkspaceErrorResponse(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error_code":"RESOURCE_DOES_NOT_EXIST","message":"nope"}`, http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	client := NewClient(srv.URL).WithContext(t.Context())
	if _, err := client.GetWorkspace("tenant"); err == nil {
		t.Fatal("expected GetWorkspace error")
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
	if c.WorkspacesEnabled() || c.WorkspaceName() != "" || c.configuredWorkspaceName() != "" {
		t.Fatal("nil client should report empty/false helpers")
	}
}
