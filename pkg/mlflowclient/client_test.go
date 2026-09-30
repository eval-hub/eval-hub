package mlflowclient

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// MLflow REST wire paths issued by mlflow-go, used to route the httptest servers.
const (
	endpointExperimentsCreate     = "/api/2.0/mlflow/experiments/create"
	endpointExperimentsGetBase    = "/api/2.0/mlflow/experiments/get"
	endpointExperimentsDeleteBase = "/api/2.0/mlflow/experiments/delete"
)

func TestCreateExperiment(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != endpointExperimentsCreate {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(CreateExperimentResponse{ExperimentID: "1"})
	}))
	t.Cleanup(srv.Close)

	client := NewClient(srv.URL).WithContext(t.Context())
	resp, err := client.CreateExperiment(&CreateExperimentRequest{Name: "demo"})
	if err != nil {
		t.Fatalf("CreateExperiment() = %v", err)
	}
	if resp.ExperimentID != "1" {
		t.Fatalf("ExperimentID = %q", resp.ExperimentID)
	}
}

func TestGetExperiment(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != endpointExperimentsGetBase {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(GetExperimentResponse{
			Experiment: Experiment{ExperimentID: "exp-9", Name: "demo"},
		})
	}))
	t.Cleanup(srv.Close)

	client := NewClient(srv.URL).WithContext(t.Context())
	resp, err := client.GetExperiment("exp-9")
	if err != nil {
		t.Fatalf("GetExperiment() = %v", err)
	}
	if resp.Experiment.ExperimentID != "exp-9" {
		t.Fatalf("ExperimentID = %q", resp.Experiment.ExperimentID)
	}
}

func TestDeleteExperiment(t *testing.T) {
	t.Parallel()
	var deleted bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == endpointExperimentsDeleteBase {
			deleted = true
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)

	client := NewClient(srv.URL).WithContext(t.Context())
	if err := client.DeleteExperiment("exp-del"); err != nil {
		t.Fatalf("DeleteExperiment() = %v", err)
	}
	if !deleted {
		t.Fatal("expected delete endpoint to be called")
	}
}

func TestGetVersion(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/version" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte("2.14.0"))
	}))
	t.Cleanup(srv.Close)

	client := NewClient(srv.URL).WithContext(t.Context())
	version, err := client.GetVersion()
	if err != nil {
		t.Fatalf("GetVersion() = %v", err)
	}
	if version != "2.14.0" {
		t.Fatalf("version = %q, want 2.14.0", version)
	}
}

func TestGetVersionErrorResponse(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error_code":"INTERNAL_ERROR","message":"boom"}`))
	}))
	t.Cleanup(srv.Close)

	client := NewClient(srv.URL).WithContext(t.Context())
	if _, err := client.GetVersion(); err == nil {
		t.Fatal("expected GetVersion error")
	}
}

func TestCreateExperimentErrorResponse(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error_code":"RESOURCE_ALREADY_EXISTS","message":"exists"}`))
	}))
	t.Cleanup(srv.Close)

	client := NewClient(srv.URL).WithContext(t.Context())
	_, err := client.CreateExperiment(&CreateExperimentRequest{Name: "demo"})
	if err == nil {
		t.Fatal("expected create experiment error")
	}
	if !IsResourceAlreadyExistsError(err) {
		t.Fatalf("error = %v, want RESOURCE_ALREADY_EXISTS", err)
	}
}

func TestGetExperimentErrorResponse(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error_code":"RESOURCE_DOES_NOT_EXIST","message":"nope"}`))
	}))
	t.Cleanup(srv.Close)

	client := NewClient(srv.URL).WithContext(t.Context())
	if _, err := client.GetExperiment("missing"); err == nil {
		t.Fatal("expected get experiment error")
	}
}

func TestDeleteExperimentErrorResponse(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error_code":"RESOURCE_DOES_NOT_EXIST","message":"nope"}`))
	}))
	t.Cleanup(srv.Close)

	client := NewClient(srv.URL).WithContext(t.Context())
	if err := client.DeleteExperiment("missing"); err == nil {
		t.Fatal("expected delete experiment error")
	}
}

// TestGetVersionNilClient exercises the nil-client guard that propagates up from
// resolveDelegate through tracking().
func TestGetVersionNilClient(t *testing.T) {
	t.Parallel()

	var c *Client
	if _, err := c.GetVersion(); err == nil {
		t.Fatal("expected error for nil client")
	}
}

// TestNewDelegateAuthOverTLS covers the token / token-path auth options that are
// only attached over a secure (HTTPS) tracking URL.
func TestNewDelegateAuthOverTLS(t *testing.T) {
	t.Parallel()

	tokenFile := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenFile, []byte("file-token"), 0o600); err != nil {
		t.Fatalf("write token file: %v", err)
	}

	var gotAuth string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(GetExperimentResponse{
			Experiment: Experiment{ExperimentID: "exp-1", Name: "demo"},
		})
	}))
	t.Cleanup(srv.Close)

	client := NewClient(srv.URL).
		WithContext(t.Context()).
		WithHTTPClient(srv.Client()).
		WithTokenPath(tokenFile).
		WithToken("static-token")
	if _, err := client.GetExperiment("exp-1"); err != nil {
		t.Fatalf("GetExperiment() = %v", err)
	}
	if gotAuth == "" {
		t.Fatal("expected Authorization header to be sent over HTTPS")
	}
}

// malformedTrackingURL fails url.Parse (invalid control character), so both
// isInsecureScheme and mlflow.NewClient reject it and the lazy delegate build in
// resolveDelegate returns an error. It exercises the delegate-build failure path
// shared by every method that resolves the delegate.
const malformedTrackingURL = "http://mlflow\x7f.invalid"

func TestDelegateBuildFailurePropagates(t *testing.T) {
	t.Parallel()

	client := NewClient(malformedTrackingURL).WithContext(t.Context())

	if _, err := client.GetExperiment("exp-1"); err == nil {
		t.Fatal("GetExperiment: expected delegate build error")
	}
	if err := client.DeleteExperiment("exp-1"); err == nil {
		t.Fatal("DeleteExperiment: expected delegate build error")
	}
	if _, err := client.ProbeWorkspacesEnabled(); err == nil {
		t.Fatal("ProbeWorkspacesEnabled: expected delegate build error")
	}
	if _, err := client.CreateWorkspace(&CreateWorkspaceRequest{Name: "ws"}); err == nil {
		t.Fatal("CreateWorkspace: expected delegate build error")
	}
	if _, err := client.GetWorkspace("ws"); err == nil {
		t.Fatal("GetWorkspace: expected delegate build error")
	}
	if _, err := client.UploadArtifact("1/run-1/artifacts/f.json", strings.NewReader("{}"), ""); err == nil {
		t.Fatal("UploadArtifact: expected delegate build error")
	}
	if reader, err := client.DownloadArtifact("1/run-1/artifacts/f.json"); err == nil {
		if reader != nil {
			_ = reader.Close()
		}
		t.Fatal("DownloadArtifact: expected delegate build error")
	}
}

// TestNewDelegateInsecureDropsAuth covers the plain-HTTP branch where auth
// credentials are dropped and the one-shot warning is emitted.
func TestNewDelegateInsecureDropsAuth(t *testing.T) {
	t.Parallel()

	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(GetExperimentResponse{
			Experiment: Experiment{ExperimentID: "exp-1", Name: "demo"},
		})
	}))
	t.Cleanup(srv.Close)

	client := NewClient(srv.URL). // plain HTTP
					WithContext(t.Context()).
					WithToken("static-token")
	if _, err := client.GetExperiment("exp-1"); err != nil {
		t.Fatalf("GetExperiment() = %v", err)
	}
	if gotAuth != "" {
		t.Fatalf("Authorization header = %q, want empty over plain HTTP", gotAuth)
	}
}
