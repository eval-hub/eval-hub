package mlflowclient

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUploadArtifact(t *testing.T) {
	t.Parallel()

	const artifactPath = "1/run-1/artifacts/evaluation-card.json"
	body := []byte(`{"card_version":"1.0"}`)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Fatalf("method = %s, want PUT", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, artifactPath) {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Fatalf("content-type = %q", got)
		}
		payload, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		if string(payload) != string(body) {
			t.Fatalf("body = %s", string(payload))
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	client := NewClient(srv.URL).WithContext(t.Context())
	url, err := client.UploadArtifact(artifactPath, body, "application/json")
	if err != nil {
		t.Fatalf("UploadArtifact() err = %v", err)
	}
	want := srv.URL + "/api/2.0/mlflow-artifacts/artifacts/1/run-1/artifacts/evaluation-card.json"
	if url != want {
		t.Fatalf("url = %q, want %q", url, want)
	}
}

func TestBuildArtifactUploadEndpoint(t *testing.T) {
	t.Parallel()

	got, err := buildArtifactUploadEndpoint("1/run 1/artifacts/evaluation-card.json")
	if err != nil {
		t.Fatalf("buildArtifactUploadEndpoint() err = %v", err)
	}
	want := "/api/2.0/mlflow-artifacts/artifacts/1/run%201/artifacts/evaluation-card.json"
	if got != want {
		t.Fatalf("endpoint = %q, want %q", got, want)
	}

	if _, err := buildArtifactUploadEndpoint(""); err == nil {
		t.Fatal("expected error for empty artifact path")
	}
	if _, err := buildArtifactUploadEndpoint("/"); err == nil {
		t.Fatal("expected error for slash-only artifact path")
	}
}

func TestUploadArtifactValidationErrors(t *testing.T) {
	t.Parallel()

	var nilClient *Client
	if _, err := nilClient.UploadArtifact("path", []byte("x"), ""); err == nil {
		t.Fatal("expected error for nil client")
	}

	client := NewClient("http://example.com").WithContext(t.Context())
	if _, err := client.UploadArtifact("", []byte("x"), ""); err == nil {
		t.Fatal("expected error for empty artifact path")
	}
}

func TestUploadArtifactErrorResponse(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error_code":"INVALID_PARAMETER_VALUE","message":"bad path"}`))
	}))
	t.Cleanup(srv.Close)

	client := NewClient(srv.URL).WithContext(t.Context())
	_, err := client.UploadArtifact("1/run-1/artifacts/file.json", []byte("{}"), "application/json")
	if err == nil {
		t.Fatal("expected upload error")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("error type = %T, want *APIError", err)
	}
	if apiErr.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d", apiErr.StatusCode)
	}
}

func TestUploadArtifactWithWorkspaceHeader(t *testing.T) {
	t.Parallel()

	var workspaceHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		workspaceHeader = r.Header.Get("X-MLFLOW-WORKSPACE")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	client := NewClient(srv.URL).
		WithContext(t.Context()).
		WithWorkspacesSupport(true).
		WithWorkspace("tenant-a")
	_, err := client.UploadArtifact("1/run-1/artifacts/file.json", []byte("{}"), "application/json")
	if err != nil {
		t.Fatalf("UploadArtifact() err = %v", err)
	}
	if workspaceHeader != "tenant-a" {
		t.Fatalf("workspace header = %q", workspaceHeader)
	}
}
