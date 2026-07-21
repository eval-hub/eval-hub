package mlflowclient

import (
	"bytes"
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
		if r.ContentLength != int64(len(body)) {
			t.Fatalf("content-length = %d, want %d", r.ContentLength, len(body))
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
	url, err := client.UploadArtifact(artifactPath, bytes.NewReader(body), "application/json")
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
	if _, err := buildArtifactUploadEndpoint("../etc/passwd"); err == nil {
		t.Fatal("expected error for path with .. segment")
	}
	if _, err := buildArtifactUploadEndpoint("1/./artifacts/file.json"); err == nil {
		t.Fatal("expected error for path with . segment")
	}
	if _, err := buildArtifactUploadEndpoint("1/../artifacts/file.json"); err == nil {
		t.Fatal("expected error for path with embedded .. segment")
	}
}

func TestUploadArtifactValidationErrors(t *testing.T) {
	t.Parallel()

	var nilClient *Client
	if _, err := nilClient.UploadArtifact("path", strings.NewReader("x"), ""); err == nil {
		t.Fatal("expected error for nil client")
	}

	client := NewClient("http://example.com").WithContext(t.Context())
	if _, err := client.UploadArtifact("", strings.NewReader("x"), ""); err == nil {
		t.Fatal("expected error for empty artifact path")
	}
	if _, err := client.UploadArtifact("path", nil, ""); err == nil {
		t.Fatal("expected error for nil reader")
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
	_, err := client.UploadArtifact("1/run-1/artifacts/file.json", strings.NewReader("{}"), "application/json")
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
	_, err := client.UploadArtifact("1/run-1/artifacts/file.json", strings.NewReader("{}"), "application/json")
	if err != nil {
		t.Fatalf("UploadArtifact() err = %v", err)
	}
	if workspaceHeader != "tenant-a" {
		t.Fatalf("workspace header = %q", workspaceHeader)
	}
}

func TestReaderContentLength(t *testing.T) {
	t.Parallel()

	body := []byte("artifact-bytes")
	if got := readerContentLength(bytes.NewReader(body)); got != int64(len(body)) {
		t.Fatalf("bytes.Reader length = %d, want %d", got, len(body))
	}
	if got := readerContentLength(strings.NewReader("artifact-bytes")); got != int64(len(body)) {
		t.Fatalf("strings.Reader length = %d, want %d", got, len(body))
	}
	if got := readerContentLength(io.NopCloser(strings.NewReader("x"))); got != -1 {
		t.Fatalf("wrapped reader length = %d, want -1", got)
	}
}

func TestUploadArtifactWithoutKnownContentLength(t *testing.T) {
	t.Parallel()

	pr, pw := io.Pipe()
	go func() {
		_, _ = io.WriteString(pw, `{"card_version":"1.0"}`)
		_ = pw.Close()
	}()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ContentLength != -1 {
			t.Fatalf("content-length = %d, want -1 for streaming reader", r.ContentLength)
		}
		payload, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		if string(payload) != `{"card_version":"1.0"}` {
			t.Fatalf("body = %q", string(payload))
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	client := NewClient(srv.URL).WithContext(t.Context())
	if _, err := client.UploadArtifact("1/run-1/artifacts/file.json", pr, "application/json"); err != nil {
		t.Fatalf("UploadArtifact() err = %v", err)
	}
}
