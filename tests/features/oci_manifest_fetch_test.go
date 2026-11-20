package features

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	pkgapi "github.com/eval-hub/eval-hub/pkg/api"
	"github.com/eval-hub/eval-hub/pkg/cards"
	"github.com/eval-hub/eval-hub/pkg/ociclient"
)

func TestFetchOCIEvaluationCard(t *testing.T) {
	// These helper regressions run without the FVT suite's log directory setup.
	once.Do(func() { logger = log.New(io.Discard, "", 0) })
	t.Setenv("OCI_USERNAME", "")
	t.Setenv("OCI_PASSWORD", "")
	t.Setenv("MODEL_NAME", "test-model")

	cardJSON, err := json.Marshal(cards.EvaluationCard{
		CardVersion:   cards.CardVersion,
		SchemaVersion: cards.SchemaVersion,
		Metadata:      cards.EvaluationCardMetadata{EvaluationJobID: "job-1"},
		Context: cards.EvaluationCardContext{
			Model: &cards.CardModelRef{Name: "test-model", URL: "http://model.example/v1"},
		},
		Results: &cards.EvaluationCardResults{
			Status: &cards.CardJobStatus{State: pkgapi.OverallStateCompleted},
			Benchmarks: []cards.CardBenchmarkResult{{
				ID: "arc_easy", ProviderID: "lm_evaluation_harness",
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256(cardJSON))
	cardLayer := map[string]any{
		"mediaType": ociclient.MediaTypeEvaluationCardLayer,
		"digest":    digest,
		"size":      len(cardJSON),
	}
	archiveLayer := map[string]any{
		"mediaType": "application/vnd.oci.image.layer.v1.tar+gzip",
		"digest":    "sha256:" + strings.Repeat("a", 64),
	}

	tests := []struct {
		name           string
		layers         []any
		manifestStatus int
		blobStatus     int
		blob           []byte
		wantError      string
		wantInvalid    bool
		wantMissing    bool
	}{
		{name: "raw JSON card", layers: []any{cardLayer}},
		{name: "card is not the first layer", layers: []any{archiveLayer, cardLayer}},
		{name: "reject adapter archive", layers: []any{archiveLayer}, wantError: "manifest has no evaluation card layer"},
		{name: "reject missing layers", wantError: "manifest layers array is empty or invalid"},
		{
			name: "reject missing card digest",
			layers: []any{map[string]any{
				"mediaType": ociclient.MediaTypeEvaluationCardLayer,
			}},
			wantError: "evaluation card layer digest is empty or invalid",
		},
		{name: "missing manifest", manifestStatus: http.StatusNotFound, wantMissing: true},
		{name: "missing blob", layers: []any{cardLayer}, blobStatus: http.StatusNotFound, wantError: "OCI blob fetch returned status 404"},
		{name: "invalid JSON is not accepted", layers: []any{cardLayer}, blob: []byte("not JSON"), wantInvalid: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v2/test/results/manifests/evaluation-card-job-1":
					if r.Header.Get("Accept") != ociclient.MediaTypeImageManifest {
						t.Errorf("unexpected manifest Accept header: %q", r.Header.Get("Accept"))
					}
					if tt.manifestStatus != 0 {
						w.WriteHeader(tt.manifestStatus)
						return
					}
					w.Header().Set("Content-Type", ociclient.MediaTypeImageManifest)
					_ = json.NewEncoder(w).Encode(map[string]any{
						"schemaVersion": 2,
						"mediaType":     ociclient.MediaTypeImageManifest,
						"annotations":   map[string]string{"team": "ml-platform"},
						"layers":        tt.layers,
					})
				case "/v2/test/results/blobs/" + digest:
					if tt.blobStatus != 0 {
						w.WriteHeader(tt.blobStatus)
						return
					}
					// Registries may serve blobs as octet-stream; the manifest
					// layer's media type identifies the evaluation card.
					w.Header().Set("Content-Type", "application/octet-stream")
					body := tt.blob
					if body == nil {
						body = cardJSON
					}
					_, _ = w.Write(body)
				default:
					t.Errorf("unexpected registry request: %s", r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer srv.Close()
			t.Setenv("OCI_REGISTRY", srv.URL)
			tc := &scenarioConfig{
				values:          map[string]string{"job_id": "job-1"},
				ociHTTPClient:   srv.Client(),
				ociArtifactBody: []byte("stale card"),
			}
			err := tc.iFetchOCIEvaluationCardManifestByRepoAndTag("test/results", "evaluation-card-{{value:job_id}}")
			if tt.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantError) {
					t.Fatalf("error = %v, want substring %q", err, tt.wantError)
				}
				if len(tc.ociArtifactBody) != 0 {
					t.Fatal("failed fetch retained stale artifact content")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tt.wantMissing {
				if err := tc.theOCIManifestShouldNotExist(); err != nil {
					t.Fatal(err)
				}
				if err := tc.theOCIArtifactShouldExist(); err == nil {
					t.Fatal("missing manifest retained stale artifact content")
				}
				return
			}
			if tt.wantInvalid {
				if err := tc.theOCIArtifactShouldBeValidJSON(); err == nil {
					t.Fatal("expected invalid JSON assertion to fail")
				}
				return
			}
			if !bytes.Equal(tc.ociArtifactBody, cardJSON) {
				t.Fatalf("artifact body = %s, want raw card JSON", tc.ociArtifactBody)
			}
			if err := tc.theOCIArtifactShouldExist(); err != nil {
				t.Fatal(err)
			}
			if err := tc.theOCIArtifactShouldBeValidJSON(); err != nil {
				t.Fatal(err)
			}
			if err := tc.theOCIManifestShouldContainAnnotation("team", "ml-platform"); err != nil {
				t.Fatal(err)
			}
			if err := tc.theOCIArtifactShouldContain("card_version"); err != nil {
				t.Fatal(err)
			}
			for path, value := range map[string]string{
				"$.metadata.evaluation_job_id": "{{value:job_id}}",
				"$.context.model.name":         "{{env:MODEL_NAME|test}}",
				"$.results.benchmarks[0].id":   "arc_easy",
				"$.results.status.state":       "completed",
			} {
				if err := tc.theOCIArtifactShouldContainValueAtPath(value, path); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestFetchOCIAdapterArchive(t *testing.T) {
	once.Do(func() { logger = log.New(io.Discard, "", 0) })
	t.Setenv("OCI_USERNAME", "")
	t.Setenv("OCI_PASSWORD", "")

	results := []byte(`{"benchmark_id":"arc_easy","model_name":"test","results":{"acc":0.5}}`)
	var archive bytes.Buffer
	tw := tar.NewWriter(&archive)
	if err := tw.WriteHeader(&tar.Header{
		Name: "results_job-1.json", Mode: 0600, Size: int64(len(results)),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(results); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	var compressed bytes.Buffer
	gw := gzip.NewWriter(&compressed)
	if _, err := gw.Write(archive.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name      string
		mediaType string
		body      []byte
	}{
		{"tar", "application/vnd.oci.image.layer.v1.tar", archive.Bytes()},
		{"gzip tar", "application/vnd.oci.image.layer.v1.tar+gzip", compressed.Bytes()},
	} {
		t.Run(tt.name, func(t *testing.T) {
			digest := fmt.Sprintf("sha256:%x", sha256.Sum256(tt.body))
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v2/test/results/manifests/adapter-job-1":
					_ = json.NewEncoder(w).Encode(map[string]any{
						"schemaVersion": 2,
						"mediaType":     ociclient.MediaTypeImageManifest,
						"layers": []any{map[string]any{
							"mediaType": tt.mediaType,
							"digest":    digest,
							"size":      len(tt.body),
						}},
					})
				case "/v2/test/results/blobs/" + digest:
					_, _ = w.Write(tt.body)
				default:
					t.Errorf("unexpected registry request: %s", r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer srv.Close()
			t.Setenv("OCI_REGISTRY", srv.URL)
			tc := &scenarioConfig{ociHTTPClient: srv.Client()}
			if err := tc.iFetchOCIManifestByRepoAndTag("test/results", "adapter-job-1"); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(tc.ociArtifactBody, results) {
				t.Fatalf("artifact body = %s, want extracted adapter results", tc.ociArtifactBody)
			}
		})
	}
}
