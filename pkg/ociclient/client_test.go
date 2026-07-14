package ociclient

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestPushEvaluationCard(t *testing.T) {
	t.Parallel()

	var uploadedManifest []byte
	var mu sync.Mutex
	tokenPath := "/token"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v2":
			w.Header().Set("WWW-Authenticate", `Bearer realm="http://`+r.Host+tokenPath+`",service="test"`)
			w.WriteHeader(http.StatusUnauthorized)
		case r.Method == http.MethodGet && r.URL.Path == tokenPath:
			_ = json.NewEncoder(w).Encode(tokenResponse{Token: "registry-token"})
		case r.Method == http.MethodHead && strings.HasPrefix(r.URL.Path, "/v2/test-org/test-repo/blobs/"):
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodPost && r.URL.Path == "/v2/test-org/test-repo/blobs/uploads/":
			w.Header().Set("Location", "/v2/test-org/test-repo/blobs/uploads/upload-1")
			w.WriteHeader(http.StatusAccepted)
		case r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/blobs/uploads/"):
			if r.URL.Query().Get("digest") == "" {
				http.NotFound(w, r)
				return
			}
			_, _ = io.Copy(io.Discard, r.Body)
			w.WriteHeader(http.StatusCreated)
		case r.Method == http.MethodPut && r.URL.Path == "/v2/test-org/test-repo/manifests/eval-1-job-1":
			mu.Lock()
			uploadedManifest, _ = io.ReadAll(r.Body)
			mu.Unlock()
			w.WriteHeader(http.StatusCreated)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	client, err := NewClient(srv.URL, "test-org/test-repo", Credentials{Username: "user", Password: "pass"}, srv.Client())
	if err != nil {
		t.Fatalf("NewClient() err = %v", err)
	}
	cardJSON := []byte(`{"card_version":"1.0"}`)
	if err := client.PushEvaluationCard(context.Background(), "job-1", cardJSON, "eval-1", map[string]string{"job": "eval-1"}); err != nil {
		t.Fatalf("PushEvaluationCard() err = %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(uploadedManifest) == 0 {
		t.Fatal("expected manifest upload")
	}
	var got manifest
	if err := json.Unmarshal(uploadedManifest, &got); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}
	if got.MediaType != MediaTypeImageManifest {
		t.Fatalf("manifest mediaType = %q", got.MediaType)
	}
	if len(got.Layers) != 1 || got.Layers[0].MediaType != MediaTypeEvaluationCardLayer {
		t.Fatalf("layers = %#v", got.Layers)
	}
	sum := sha256.Sum256(cardJSON)
	wantDigest := "sha256:" + hex.EncodeToString(sum[:])
	if got.Layers[0].Digest != wantDigest {
		t.Fatalf("layer digest = %q want %q", got.Layers[0].Digest, wantDigest)
	}
	if got.Annotations[AnnotationEvaluationJobID] != "job-1" {
		t.Fatalf("manifest evaluation_job_id = %q", got.Annotations[AnnotationEvaluationJobID])
	}
	if got.Annotations["job"] != "eval-1" {
		t.Fatalf("annotations = %#v", got.Annotations)
	}
	if len(got.Layers) != 1 {
		t.Fatalf("layers = %#v", got.Layers)
	}
	if got.Layers[0].Annotations[AnnotationImageTitle] != "evaluation-card-job-1.json" {
		t.Fatalf("layer title = %q", got.Layers[0].Annotations[AnnotationImageTitle])
	}
	if got.Config.Annotations[AnnotationImageTitle] != "evaluation-card-job-1-config.json" {
		t.Fatalf("config title = %q", got.Config.Annotations[AnnotationImageTitle])
	}
}

func TestPushEvaluationCardSkipsExistingBlob(t *testing.T) {
	t.Parallel()

	uploads := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v2":
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodHead && strings.HasPrefix(r.URL.Path, "/v2/test-org/test-repo/blobs/"):
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && r.URL.Path == "/v2/test-org/test-repo/blobs/uploads/":
			uploads++
			http.NotFound(w, r)
		case r.Method == http.MethodPut && r.URL.Path == "/v2/test-org/test-repo/manifests/evaluation-card-job-1":
			w.WriteHeader(http.StatusCreated)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	client, err := NewClient(srv.URL, "test-org/test-repo", Credentials{}, srv.Client())
	if err != nil {
		t.Fatalf("NewClient() err = %v", err)
	}
	if err := client.PushEvaluationCard(context.Background(), "job-1", []byte(`{"card_version":"1.0"}`), "", nil); err != nil {
		t.Fatalf("PushEvaluationCard() err = %v", err)
	}
	if uploads != 0 {
		t.Fatalf("uploads = %d, want 0 when blobs already exist", uploads)
	}
}

func TestUploadBlobChunked(t *testing.T) {
	t.Parallel()

	const chunkSize = 1024
	payload := bytes.Repeat([]byte("a"), chunkSize*3+17)
	var patchRanges []string
	var finalizeDigest string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v2":
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodHead && strings.HasPrefix(r.URL.Path, "/v2/test-org/test-repo/blobs/"):
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodPost && r.URL.Path == "/v2/test-org/test-repo/blobs/uploads/":
			w.Header().Set("Location", "/v2/test-org/test-repo/blobs/uploads/upload-1")
			w.WriteHeader(http.StatusAccepted)
		case r.Method == http.MethodPatch && strings.Contains(r.URL.Path, "/blobs/uploads/"):
			patchRanges = append(patchRanges, r.Header.Get("Content-Range"))
			_, _ = io.Copy(io.Discard, r.Body)
			w.WriteHeader(http.StatusAccepted)
		case r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/blobs/uploads/"):
			finalizeDigest = r.URL.Query().Get("digest")
			w.WriteHeader(http.StatusCreated)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	client, err := NewClient(srv.URL, "test-org/test-repo", Credentials{}, srv.Client())
	if err != nil {
		t.Fatalf("NewClient() err = %v", err)
	}

	digest, size, err := client.UploadBlob(context.Background(), bytes.NewReader(payload), UploadBlobOptions{ChunkSize: chunkSize})
	if err != nil {
		t.Fatalf("UploadBlob() err = %v", err)
	}
	sum := sha256.Sum256(payload)
	wantDigest := "sha256:" + hex.EncodeToString(sum[:])
	if digest != wantDigest {
		t.Fatalf("digest = %q, want %q", digest, wantDigest)
	}
	if size != int64(len(payload)) {
		t.Fatalf("size = %d, want %d", size, len(payload))
	}
	if finalizeDigest != wantDigest {
		t.Fatalf("finalize digest = %q, want %q", finalizeDigest, wantDigest)
	}
	if len(patchRanges) != 4 {
		t.Fatalf("patch ranges = %v, want 4 chunks", patchRanges)
	}
	if patchRanges[0] != "0-1023" || patchRanges[3] != "3072-3088" {
		t.Fatalf("unexpected chunk ranges: %v", patchRanges)
	}
}

func TestUploadBlobSkipsKnownDigest(t *testing.T) {
	t.Parallel()

	uploads := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v2":
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodHead && strings.HasPrefix(r.URL.Path, "/v2/test-org/test-repo/blobs/"):
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && r.URL.Path == "/v2/test-org/test-repo/blobs/uploads/":
			uploads++
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	client, err := NewClient(srv.URL, "test-org/test-repo", Credentials{}, srv.Client())
	if err != nil {
		t.Fatalf("NewClient() err = %v", err)
	}
	payload := []byte("already-there")
	sum := sha256.Sum256(payload)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	gotDigest, _, err := client.UploadBlob(context.Background(), bytes.NewReader(payload), UploadBlobOptions{KnownDigest: digest})
	if err != nil {
		t.Fatalf("UploadBlob() err = %v", err)
	}
	if gotDigest != digest {
		t.Fatalf("digest = %q, want %q", gotDigest, digest)
	}
	if uploads != 0 {
		t.Fatalf("uploads = %d, want 0 when known digest already exists", uploads)
	}
}
