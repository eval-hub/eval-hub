package features

import (
	"archive/tar"
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// TestOCIManifestFetchKeepsParentResponse ensures child failures cannot replace the requested index response.
func TestOCIManifestFetchKeepsParentResponse(t *testing.T) {
	// Scenario logging expects a bin directory beneath the working directory.
	t.Chdir(t.TempDir())
	if err := os.Mkdir("bin", 0700); err != nil {
		t.Fatal(err)
	}
	const index = `{"mediaType":"application/vnd.oci.image.index.v1+json","annotations":{"evaluation_job_id":"job"},"manifests":[{"digest":"sha256:child","annotations":{"artifact_role":"benchmark"}}]}`
	for _, scenario := range []string{"success", "missing child", "invalid child", "nested index"} {
		t.Run(scenario, func(t *testing.T) {
			var blob bytes.Buffer
			writer := tar.NewWriter(&blob)
			content := []byte(`{"score":1}`)
			if err := writer.WriteHeader(&tar.Header{Name: "results.json", Mode: 0600, Size: int64(len(content))}); err != nil {
				t.Fatal(err)
			}
			if _, err := writer.Write(content); err != nil {
				t.Fatal(err)
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v2/results/manifests/job":
					_, _ = fmt.Fprint(w, index)
				case "/v2/results/manifests/sha256:child":
					switch scenario {
					case "missing child":
						http.NotFound(w, r)
					case "invalid child":
						_, _ = fmt.Fprint(w, "invalid JSON")
					case "nested index":
						_, _ = fmt.Fprint(w, index)
					default:
						_, _ = fmt.Fprint(w, `{"mediaType":"application/vnd.oci.image.manifest.v1+json","layers":[{"digest":"sha256:blob"}]}`)
					}
				case "/v2/results/blobs/sha256:blob":
					_, _ = w.Write(blob.Bytes())
				default:
					http.NotFound(w, r)
				}
			}))
			defer registry.Close()
			t.Setenv("OCI_REGISTRY", registry.URL)
			t.Setenv("OCI_USERNAME", "")
			tc := &scenarioConfig{}
			err := tc.iFetchOCIManifestByRepoAndTag("results", "job")
			if (err == nil) != (scenario == "success") {
				t.Fatalf("unexpected fetch outcome: %v", err)
			}
			if tc.ociManifestStatusCode != http.StatusOK || tc.ociManifestError != nil || string(tc.ociManifestBody) != index {
				t.Fatalf("child fetch changed parent response: status=%d error=%v body=%s", tc.ociManifestStatusCode, tc.ociManifestError, tc.ociManifestBody)
			}
			if tc.ociManifestData["mediaType"] != "application/vnd.oci.image.index.v1+json" {
				t.Fatal("parent parsed manifest was replaced")
			}
			if scenario == "success" && !bytes.Equal(tc.ociArtifactBody, content) {
				t.Fatalf("unexpected benchmark content: %s", tc.ociArtifactBody)
			}
			if err := tc.iFetchOCIManifestByRepoAndTag("results", "missing"); err != nil {
				t.Fatalf("expected HTTP failure should be available to assertion steps: %v", err)
			}
			if tc.ociManifestStatusCode != http.StatusNotFound || tc.ociManifestError == nil || tc.ociManifestData != nil {
				t.Fatal("missing parent retained stale manifest state")
			}
		})
	}
}
