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

func TestUploadBlobChunkedFollowsRotatedLocation(t *testing.T) {
	t.Parallel()

	const chunkSize = 1024
	payload := bytes.Repeat([]byte("b"), chunkSize*2+10)
	var patchURLs []string
	var finalizeURL string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v2":
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodHead && strings.HasPrefix(r.URL.Path, "/v2/test-org/test-repo/blobs/"):
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodPost && r.URL.Path == "/v2/test-org/test-repo/blobs/uploads/":
			w.Header().Set("Location", "/v2/test-org/test-repo/blobs/uploads/upload-1")
			w.WriteHeader(http.StatusAccepted)
		case r.Method == http.MethodPatch && r.URL.Path == "/v2/test-org/test-repo/blobs/uploads/upload-1":
			patchURLs = append(patchURLs, r.URL.Path)
			w.Header().Set("Location", "/v2/test-org/test-repo/blobs/uploads/upload-2")
			_, _ = io.Copy(io.Discard, r.Body)
			w.WriteHeader(http.StatusAccepted)
		case r.Method == http.MethodPatch && r.URL.Path == "/v2/test-org/test-repo/blobs/uploads/upload-2":
			patchURLs = append(patchURLs, r.URL.Path)
			w.Header().Set("Location", "http://"+r.Host+"/v2/test-org/test-repo/blobs/uploads/upload-3")
			_, _ = io.Copy(io.Discard, r.Body)
			w.WriteHeader(http.StatusAccepted)
		case r.Method == http.MethodPatch && r.URL.Path == "/v2/test-org/test-repo/blobs/uploads/upload-3":
			patchURLs = append(patchURLs, r.URL.Path)
			_, _ = io.Copy(io.Discard, r.Body)
			w.WriteHeader(http.StatusAccepted)
		case r.Method == http.MethodPut && r.URL.Path == "/v2/test-org/test-repo/blobs/uploads/upload-3":
			finalizeURL = r.URL.Path
			w.WriteHeader(http.StatusCreated)
		case r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/blobs/uploads/"):
			t.Errorf("unexpected finalize URL %q", r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
			return
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	client, err := NewClient(srv.URL, "test-org/test-repo", Credentials{}, srv.Client())
	if err != nil {
		t.Fatalf("NewClient() err = %v", err)
	}

	if _, _, err := client.UploadBlob(context.Background(), bytes.NewReader(payload), UploadBlobOptions{ChunkSize: chunkSize}); err != nil {
		t.Fatalf("UploadBlob() err = %v", err)
	}
	if len(patchURLs) != 3 {
		t.Fatalf("patch URLs = %v, want 3 PATCH requests", patchURLs)
	}
	if patchURLs[0] != "/v2/test-org/test-repo/blobs/uploads/upload-1" {
		t.Fatalf("first PATCH URL = %q", patchURLs[0])
	}
	if patchURLs[1] != "/v2/test-org/test-repo/blobs/uploads/upload-2" {
		t.Fatalf("second PATCH URL = %q", patchURLs[1])
	}
	if patchURLs[2] != "/v2/test-org/test-repo/blobs/uploads/upload-3" {
		t.Fatalf("third PATCH URL = %q", patchURLs[2])
	}
	if finalizeURL != "/v2/test-org/test-repo/blobs/uploads/upload-3" {
		t.Fatalf("finalize URL = %q", finalizeURL)
	}
}

func TestUploadLocationFromResponse(t *testing.T) {
	t.Parallel()

	client := &Client{registry: "https://registry.example"}

	t.Run("missing location keeps current URL", func(t *testing.T) {
		t.Parallel()
		resp := &http.Response{Header: http.Header{}}
		got, err := client.uploadLocationFromResponse(resp, "https://registry.example/upload-1")
		if err != nil || got != "https://registry.example/upload-1" {
			t.Fatalf("uploadLocationFromResponse() = %q err=%v", got, err)
		}
	})

	t.Run("relative location resolves against registry", func(t *testing.T) {
		t.Parallel()
		resp := &http.Response{Header: http.Header{"Location": {"/v2/org/repo/blobs/uploads/upload-2"}}}
		got, err := client.uploadLocationFromResponse(resp, "https://registry.example/upload-1")
		if err != nil || got != "https://registry.example/v2/org/repo/blobs/uploads/upload-2" {
			t.Fatalf("uploadLocationFromResponse() = %q err=%v", got, err)
		}
	})
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

func TestNewClientValidationErrors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		registry   string
		repository string
		client     *http.Client
	}{
		{name: "missing registry", registry: "", repository: "org/repo", client: http.DefaultClient},
		{name: "missing repository", registry: "quay.io", repository: "", client: http.DefaultClient},
		{name: "missing http client", registry: "quay.io", repository: "org/repo", client: nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := NewClient(tc.registry, tc.repository, Credentials{}, tc.client); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestPushEvaluationCardValidationErrors(t *testing.T) {
	t.Parallel()

	client, err := NewClient("https://quay.io", "org/repo", Credentials{}, http.DefaultClient)
	if err != nil {
		t.Fatalf("NewClient() err = %v", err)
	}
	cases := []struct {
		name     string
		jobID    string
		cardJSON []byte
	}{
		{name: "missing job id", jobID: "", cardJSON: []byte(`{"card_version":"1.0"}`)},
		{name: "empty card", jobID: "job-1", cardJSON: nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := client.PushEvaluationCard(context.Background(), tc.jobID, tc.cardJSON, "", nil); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestPushEvaluationCardRetriesAuthOnUnauthorized(t *testing.T) {
	t.Parallel()

	const tokenPath = "/token"
	var headAttempts int
	var firstAuth, retryAuth string
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v2":
			w.Header().Set("WWW-Authenticate", `Bearer realm="http://`+r.Host+tokenPath+`",service="test"`)
			w.WriteHeader(http.StatusUnauthorized)
		case r.Method == http.MethodGet && r.URL.Path == tokenPath:
			_ = json.NewEncoder(w).Encode(tokenResponse{Token: "registry-token"})
		case r.Method == http.MethodHead && strings.HasPrefix(r.URL.Path, "/v2/org/repo/blobs/"):
			mu.Lock()
			headAttempts++
			switch headAttempts {
			case 1:
				firstAuth = r.Header.Get("Authorization")
			case 2:
				retryAuth = r.Header.Get("Authorization")
			}
			mu.Unlock()
			if headAttempts == 1 {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodPost && r.URL.Path == "/v2/org/repo/blobs/uploads/":
			w.Header().Set("Location", "/v2/org/repo/blobs/uploads/upload-1")
			w.WriteHeader(http.StatusAccepted)
		case r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/blobs/uploads/"):
			w.WriteHeader(http.StatusCreated)
		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/v2/org/repo/manifests/"):
			w.WriteHeader(http.StatusCreated)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	client, err := NewClient(srv.URL, "org/repo", Credentials{Username: "user", Password: "pass"}, srv.Client())
	if err != nil {
		t.Fatalf("NewClient() err = %v", err)
	}
	if err := client.PushEvaluationCard(context.Background(), "job-1", []byte(`{"card_version":"1.0"}`), "", nil); err != nil {
		t.Fatalf("PushEvaluationCard() err = %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if headAttempts < 2 {
		t.Fatalf("headAttempts = %d, want at least 2 for auth retry", headAttempts)
	}
	if firstAuth != "" {
		t.Fatalf("first blob head Authorization = %q, want empty", firstAuth)
	}
	if retryAuth != "Bearer registry-token" {
		t.Fatalf("retry blob head Authorization = %q", retryAuth)
	}
}

func TestPutManifestRetriesAuthWithReplayableBody(t *testing.T) {
	t.Parallel()

	const tokenPath = "/token"
	var putAttempts int
	var replayedBody bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v2":
			w.Header().Set("WWW-Authenticate", `Bearer realm="http://`+r.Host+tokenPath+`",service="test"`)
			w.WriteHeader(http.StatusUnauthorized)
		case r.Method == http.MethodGet && r.URL.Path == tokenPath:
			_ = json.NewEncoder(w).Encode(tokenResponse{Token: "registry-token"})
		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/v2/org/repo/manifests/"):
			putAttempts++
			if putAttempts == 1 {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			body, _ := io.ReadAll(r.Body)
			if len(body) > 0 {
				replayedBody = true
			}
			w.WriteHeader(http.StatusCreated)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	client, err := NewClient(srv.URL, "org/repo", Credentials{Username: "user", Password: "pass"}, srv.Client())
	if err != nil {
		t.Fatalf("NewClient() err = %v", err)
	}
	if err := client.putManifest(context.Background(), "evaluation-card-job-1", []byte(`{"schemaVersion":2}`)); err != nil {
		t.Fatalf("putManifest() err = %v", err)
	}
	if putAttempts != 2 {
		t.Fatalf("putAttempts = %d, want 2", putAttempts)
	}
	if !replayedBody {
		t.Fatal("expected replayed manifest body on auth retry")
	}
}

func TestEnsureBlobUsesMonolithicUploadForSmallContent(t *testing.T) {
	t.Parallel()

	var patchCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v2":
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodHead && strings.HasPrefix(r.URL.Path, "/v2/org/repo/blobs/"):
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodPost && r.URL.Path == "/v2/org/repo/blobs/uploads/":
			w.Header().Set("Location", "/v2/org/repo/blobs/uploads/upload-1")
			w.WriteHeader(http.StatusAccepted)
		case r.Method == http.MethodPatch:
			patchCalls++
			http.NotFound(w, r)
		case r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/blobs/uploads/"):
			w.WriteHeader(http.StatusCreated)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	client, err := NewClient(srv.URL, "org/repo", Credentials{}, srv.Client())
	if err != nil {
		t.Fatalf("NewClient() err = %v", err)
	}
	content := []byte("small-blob")
	digest, size, err := client.ensureBlob(context.Background(), content)
	if err != nil {
		t.Fatalf("ensureBlob() err = %v", err)
	}
	if digest != blobDigest(content) || size != int64(len(content)) {
		t.Fatalf("digest=%q size=%d", digest, size)
	}
	if patchCalls != 0 {
		t.Fatalf("patchCalls = %d, want monolithic upload without PATCH", patchCalls)
	}
}

func TestEnsureBlobUsesChunkedUploadForLargeContent(t *testing.T) {
	t.Parallel()

	var patchCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v2":
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodHead && strings.HasPrefix(r.URL.Path, "/v2/org/repo/blobs/"):
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodPost && r.URL.Path == "/v2/org/repo/blobs/uploads/":
			w.Header().Set("Location", "/v2/org/repo/blobs/uploads/upload-1")
			w.WriteHeader(http.StatusAccepted)
		case r.Method == http.MethodPatch && strings.Contains(r.URL.Path, "/blobs/uploads/"):
			patchCalls++
			_, _ = io.Copy(io.Discard, r.Body)
			w.WriteHeader(http.StatusAccepted)
		case r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/blobs/uploads/"):
			w.WriteHeader(http.StatusCreated)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	client, err := NewClient(srv.URL, "org/repo", Credentials{}, srv.Client())
	if err != nil {
		t.Fatalf("NewClient() err = %v", err)
	}
	content := bytes.Repeat([]byte("x"), DefaultChunkSize+1)
	digest, size, err := client.ensureBlob(context.Background(), content)
	if err != nil {
		t.Fatalf("ensureBlob() err = %v", err)
	}
	if digest != blobDigest(content) || size != int64(len(content)) {
		t.Fatalf("digest=%q size=%d", digest, size)
	}
	if patchCalls == 0 {
		t.Fatal("expected chunked PATCH upload for large content")
	}
}

func TestResolveLocation(t *testing.T) {
	t.Parallel()

	client := &Client{registry: "https://registry.example"}
	abs, err := client.resolveLocation("https://auth.example/upload/session-1")
	if err != nil || abs != "https://auth.example/upload/session-1" {
		t.Fatalf("resolveLocation(abs) = %q err=%v", abs, err)
	}
	rel, err := client.resolveLocation("/v2/org/repo/blobs/uploads/session-1")
	if err != nil || rel != "https://registry.example/v2/org/repo/blobs/uploads/session-1" {
		t.Fatalf("resolveLocation(rel) = %q err=%v", rel, err)
	}
}

func TestBlobExistsServerError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)

	client, err := NewClient(srv.URL, "org/repo", Credentials{}, srv.Client())
	if err != nil {
		t.Fatalf("NewClient() err = %v", err)
	}
	if _, err := client.blobExists(context.Background(), "sha256:deadbeef"); err == nil {
		t.Fatal("expected blob head error")
	}
}

func TestStartBlobUploadMissingLocation(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/blobs/uploads/") {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)

	client, err := NewClient(srv.URL, "org/repo", Credentials{}, srv.Client())
	if err != nil {
		t.Fatalf("NewClient() err = %v", err)
	}
	if _, err := client.startBlobUpload(context.Background()); err == nil {
		t.Fatal("expected missing Location header error")
	}
}

func TestPutManifestError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/v2/org/repo/manifests/") {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)

	client, err := NewClient(srv.URL, "org/repo", Credentials{}, srv.Client())
	if err != nil {
		t.Fatalf("NewClient() err = %v", err)
	}
	if err := client.putManifest(context.Background(), "tag-1", []byte(`{}`)); err == nil {
		t.Fatal("expected put manifest error")
	}
}

func TestUploadBlobNilReader(t *testing.T) {
	t.Parallel()

	client, err := NewClient("https://quay.io", "org/repo", Credentials{}, http.DefaultClient)
	if err != nil {
		t.Fatalf("NewClient() err = %v", err)
	}
	if _, _, err := client.UploadBlob(context.Background(), nil, UploadBlobOptions{}); err == nil {
		t.Fatal("expected error for nil reader")
	}
}

func TestRegistryURLAbsolutePath(t *testing.T) {
	t.Parallel()

	client := &Client{registry: "https://registry.example"}
	if got := client.registryURL("https://auth.example/token"); got != "https://auth.example/token" {
		t.Fatalf("registryURL() = %q", got)
	}
}

func TestResolveLocationInvalidRegistry(t *testing.T) {
	t.Parallel()

	client := &Client{registry: "://bad-registry"}
	if _, err := client.resolveLocation("/upload"); err == nil {
		t.Fatal("expected registry parse error")
	}
}

func TestResolveLocationInvalidLocation(t *testing.T) {
	t.Parallel()

	client := &Client{registry: "https://registry.example"}
	if _, err := client.resolveLocation("://bad-location"); err == nil {
		t.Fatal("expected location parse error")
	}
}

func TestDoRefreshTokenFailure(t *testing.T) {
	t.Parallel()

	const tokenPath = "/token"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodHead:
			w.WriteHeader(http.StatusUnauthorized)
		case r.Method == http.MethodGet && r.URL.Path == "/v2":
			w.Header().Set("WWW-Authenticate", `Bearer realm="http://`+r.Host+tokenPath+`",service="test"`)
			w.WriteHeader(http.StatusUnauthorized)
		case r.Method == http.MethodGet && r.URL.Path == tokenPath:
			w.WriteHeader(http.StatusUnauthorized)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	client, err := NewClient(srv.URL, "org/repo", Credentials{Username: "user", Password: "bad"}, srv.Client())
	if err != nil {
		t.Fatalf("NewClient() err = %v", err)
	}
	if _, err := client.blobExists(context.Background(), "sha256:deadbeef"); err == nil {
		t.Fatal("expected auth refresh failure")
	}
}

func TestUploadBlobMonolithicFailure(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v2":
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodHead && strings.HasPrefix(r.URL.Path, "/v2/org/repo/blobs/"):
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodPost && r.URL.Path == "/v2/org/repo/blobs/uploads/":
			w.Header().Set("Location", "/v2/org/repo/blobs/uploads/upload-1")
			w.WriteHeader(http.StatusAccepted)
		case r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/blobs/uploads/"):
			w.WriteHeader(http.StatusBadRequest)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	client, err := NewClient(srv.URL, "org/repo", Credentials{}, srv.Client())
	if err != nil {
		t.Fatalf("NewClient() err = %v", err)
	}
	if err := client.PushEvaluationCard(context.Background(), "job-1", []byte(`{"card_version":"1.0"}`), "", nil); err == nil {
		t.Fatal("expected upload failure")
	}
}

func TestPatchBlobChunkFailure(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v2":
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && r.URL.Path == "/v2/org/repo/blobs/uploads/":
			w.Header().Set("Location", "/v2/org/repo/blobs/uploads/upload-1")
			w.WriteHeader(http.StatusAccepted)
		case r.Method == http.MethodPatch:
			w.WriteHeader(http.StatusBadRequest)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	client, err := NewClient(srv.URL, "org/repo", Credentials{}, srv.Client())
	if err != nil {
		t.Fatalf("NewClient() err = %v", err)
	}
	if _, _, err := client.UploadBlob(context.Background(), bytes.NewReader([]byte("chunked")), UploadBlobOptions{ChunkSize: 4}); err == nil {
		t.Fatal("expected patch failure")
	}
}
