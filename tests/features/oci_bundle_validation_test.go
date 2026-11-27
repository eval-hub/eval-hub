package features

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/eval-hub/eval-hub/pkg/ociclient"
)

// Exercise the actual FVT steps against a registry, including descriptors whose
// metadata is plausible but whose referenced bytes or API association are wrong.
func TestOCIBundleValidation(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.Mkdir("bin", 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OCI_USERNAME", "")
	t.Setenv("OCI_PASSWORD", "")
	digest := func(b []byte) string { return fmt.Sprintf("sha256:%x", sha256.Sum256(b)) }
	encode := func(v any) []byte {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	card := []byte(`{"metadata":{"evaluation_job_id":"job"},"results":{"status":{"state":"completed"}}}`)
	adapterJSON := []byte(`{"id":"job","results":{}}`)
	var archive bytes.Buffer
	writer := tar.NewWriter(&archive)
	if err := writer.WriteHeader(&tar.Header{Name: "results.json", Mode: 0600, Size: int64(len(adapterJSON))}); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(adapterJSON); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	adapter := archive.Bytes()
	manifest := func(payload []byte, media string) []byte {
		return encode(map[string]any{"schemaVersion": 2, "mediaType": ociclient.MediaTypeImageManifest, "layers": []ociclient.Descriptor{{MediaType: media, Digest: digest(payload), Size: int64(len(payload))}}})
	}
	cardManifest := manifest(card, ociclient.MediaTypeEvaluationCardLayer)
	adapterManifest := manifest(adapter, "application/vnd.oci.image.layer.v1.tar")
	for _, name := range []string{"valid", "missing card", "missing adapter", "wrong role", "wrong API card", "wrong API adapter", "wrong size", "corrupt child", "wrong benchmark", "swapped children", "wrong job", "wrong bundle digest", "missing registry child"} {
		t.Run(name, func(t *testing.T) {
			index := ociclient.Index{SchemaVersion: 2, MediaType: ociclient.MediaTypeImageIndex, Annotations: map[string]string{"evaluation_job_id": "job", "evaluation_state": "completed", "included_benchmark_count": "1", "skipped_benchmark_count": "0"}, Manifests: []ociclient.Descriptor{
				{MediaType: ociclient.MediaTypeImageManifest, Digest: digest(cardManifest), Size: int64(len(cardManifest)), Annotations: map[string]string{"artifact_role": "evaluation-card"}},
				{MediaType: ociclient.MediaTypeImageManifest, Digest: digest(adapterManifest), Size: int64(len(adapterManifest)), Annotations: map[string]string{"artifact_role": "benchmark", "benchmark_id": "arc_easy", "provider_id": "lm_evaluation_harness", "benchmark_index": "0"}},
			}}
			switch name {
			case "missing card":
				index.Manifests = index.Manifests[1:]
			case "missing adapter":
				index.Manifests = index.Manifests[:1]
			case "wrong role":
				index.Manifests[0].Annotations["artifact_role"] = "benchmark"
			case "wrong size":
				index.Manifests[1].Size++
			case "wrong benchmark":
				index.Manifests[1].Annotations["benchmark_id"] = "other"
			case "swapped children":
				index.Manifests[0], index.Manifests[1] = index.Manifests[1], index.Manifests[0]
			case "wrong job":
				index.Annotations["evaluation_job_id"] = "other"
			}
			indexBody := encode(index)
			blobs := map[string][]byte{digest(cardManifest): cardManifest, digest(adapterManifest): adapterManifest, digest(card): card, digest(adapter): adapter, digest(indexBody): indexBody}
			if name == "corrupt child" {
				blobs[digest(cardManifest)] = append(append([]byte{}, cardManifest...), ' ')
			}
			if name == "missing registry child" {
				delete(blobs, digest(cardManifest))
			}
			registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				key := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
				b, ok := blobs[key]
				if !ok {
					http.NotFound(w, r)
					return
				}
				_, _ = w.Write(b)
			}))
			defer registry.Close()
			t.Setenv("OCI_REGISTRY", registry.URL)
			cardDigest, adapterDigest, bundleDigest := digest(cardManifest), digest(adapterManifest), digest(indexBody)
			if name == "wrong API card" {
				cardDigest = digest([]byte("other card"))
			}
			if name == "wrong API adapter" {
				adapterDigest = digest([]byte("other adapter"))
			}
			if name == "wrong bundle digest" {
				bundleDigest = digest([]byte("other bundle"))
			}
			job := map[string]any{
				"resource": map[string]any{"id": "job"}, "status": map[string]any{"state": "completed"},
				"exports": map[string]any{"oci": map[string]any{"coordinates": map[string]any{"oci_host": registry.URL, "oci_repository": "results", "oci_tag": "test-v1"}}},
				"results": map[string]any{
					"benchmarks": []any{map[string]any{"id": "arc_easy", "provider_id": "lm_evaluation_harness", "benchmark_index": 0, "artifacts": map[string]any{"oci_digest": adapterDigest}}},
					"oci":        map[string]any{"status": map[string]any{"state": "completed"}, "evaluation_card": map[string]any{"oci_digest": cardDigest}, "evaluation_bundle": map[string]any{"oci_digest": bundleDigest, "oci_reference": strings.TrimPrefix(registry.URL, "http://") + "/results:test-v1-job@" + bundleDigest}},
				},
			}
			tc := &scenarioConfig{body: encode(job), ociManifestBody: indexBody}
			err := tc.theOCIBundleShouldMatchCompletedJob()
			if err == nil {
				err = tc.theOCIBundleChildShouldMatchJob(0, "evaluation-card")
			}
			if err == nil {
				err = tc.theOCIBundleChildShouldMatchJob(1, "benchmark")
			}
			if (err == nil) != (name == "valid") {
				t.Fatalf("unexpected validation result: %v", err)
			}
		})
	}
}
