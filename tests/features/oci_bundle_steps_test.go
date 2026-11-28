package features

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/eval-hub/eval-hub/pkg/ociclient"
)

// bundleJob reads the API response retained by the scenario, independently of OCI fetches.
func (tc *scenarioConfig) bundleJob() (*api.EvaluationJobResource, error) {
	var job api.EvaluationJobResource
	if err := json.Unmarshal(tc.body, &job); err != nil {
		return nil, err
	}
	if job.Results == nil || job.Results.OCI == nil || job.Results.OCI.EvaluationBundle == nil || job.Results.OCI.EvaluationCard == nil {
		return nil, fmt.Errorf("job response must contain OCI bundle and card references")
	}
	return &job, nil
}

func (tc *scenarioConfig) theOCIBundleShouldMatchCompletedJob() error {
	job, err := tc.bundleJob()
	if err != nil {
		return err
	}
	var index ociclient.Index
	if err := json.Unmarshal(tc.ociManifestBody, &index); err != nil {
		return err
	}
	if index.SchemaVersion != 2 || index.MediaType != ociclient.MediaTypeImageIndex || len(index.Manifests) != 2 {
		return fmt.Errorf("expected OCI schema 2 index with exactly two children, got %+v", index)
	}
	if len(job.Results.Benchmarks) != 1 || job.Status.State != "completed" || job.Results.OCI.Status == nil || job.Results.OCI.Status.State != "completed" {
		return fmt.Errorf("expected one benchmark result and completed job and OCI export")
	}
	for key, want := range map[string]string{"evaluation_job_id": job.Resource.ID, "evaluation_state": "completed", "included_benchmark_count": "1", "skipped_benchmark_count": "0"} {
		if index.Annotations[key] != want {
			return fmt.Errorf("bundle annotation %s = %q, want %q", key, index.Annotations[key], want)
		}
	}
	ref := job.Results.OCI.EvaluationBundle
	if err := verifyBundleBytes(tc.ociManifestBody, ref.OCIDigest, int64(len(tc.ociManifestBody))); err != nil {
		return fmt.Errorf("bundle tag does not match API digest: %w", err)
	}
	if job.Exports == nil || job.Exports.OCI == nil {
		return fmt.Errorf("job has no OCI export coordinates")
	}
	coordinates := job.Exports.OCI.Coordinates
	tag, err := ociclient.EvaluationBundleManifestTag(job.Resource.ID, coordinates.OCITag)
	if err != nil {
		return err
	}
	hostValue := coordinates.OCIHost
	if !strings.Contains(hostValue, "://") {
		hostValue = "https://" + hostValue
	}
	host, err := url.Parse(hostValue)
	if err != nil {
		return err
	}
	expectedRef := fmt.Sprintf("%s/%s:%s@%s", host.Host, coordinates.OCIRepository, tag, ref.OCIDigest)
	if ref.OCIReference != expectedRef {
		return fmt.Errorf("bundle reference = %q, want %q", ref.OCIReference, expectedRef)
	}
	fetched, err := tc.fetchOCIManifest(coordinates.OCIRepository, ref.OCIDigest, false)
	if err != nil {
		return err
	}
	return verifyBundleBytes(fetched.body, ref.OCIDigest, int64(len(tc.ociManifestBody)))
}

func verifyBundleBytes(body []byte, digest string, size int64) error {
	actual := fmt.Sprintf("sha256:%x", sha256.Sum256(body))
	if actual != digest || int64(len(body)) != size {
		return fmt.Errorf("content digest/size = %s/%d, want %s/%d", actual, len(body), digest, size)
	}
	return nil
}

// theOCIBundleChildShouldMatchJob follows the index descriptor, rather than a child tag.
// It leaves the selected child's payload available to the existing artifact assertions.
func (tc *scenarioConfig) theOCIBundleChildShouldMatchJob(child int, role string) error {
	job, err := tc.bundleJob()
	if err != nil {
		return err
	}
	var index ociclient.Index
	if err := json.Unmarshal(tc.ociManifestBody, &index); err != nil {
		return err
	}
	if child < 0 || child >= len(index.Manifests) {
		return fmt.Errorf("bundle child %d missing", child)
	}
	descriptor := index.Manifests[child]
	var expectedDigest string
	switch role {
	case "evaluation-card":
		if child != 0 {
			return fmt.Errorf("evaluation card must be child 0")
		}
		expectedDigest = job.Results.OCI.EvaluationCard.OCIDigest
	case "benchmark":
		if child != 1 || len(job.Results.Benchmarks) != 1 {
			return fmt.Errorf("expected single benchmark at child 1")
		}
		result := job.Results.Benchmarks[0]
		expectedDigest, _ = result.Artifacts["oci_digest"].(string)
		for key, want := range map[string]string{"benchmark_id": result.ID, "provider_id": result.ProviderID, "benchmark_index": fmt.Sprint(result.BenchmarkIndex)} {
			if descriptor.Annotations[key] != want {
				return fmt.Errorf("child %d annotation %s = %q, want %q", child, key, descriptor.Annotations[key], want)
			}
		}
	default:
		return fmt.Errorf("unsupported bundle role %q", role)
	}
	if expectedDigest == "" || descriptor.Digest != expectedDigest || descriptor.Annotations["artifact_role"] != role || descriptor.MediaType != ociclient.MediaTypeImageManifest {
		return fmt.Errorf("child %d does not match %s API artifact: %+v", child, role, descriptor)
	}
	if job.Exports == nil || job.Exports.OCI == nil {
		return fmt.Errorf("job has no OCI export coordinates")
	}
	repository := job.Exports.OCI.Coordinates.OCIRepository
	manifest, err := tc.fetchOCIManifest(repository, descriptor.Digest, role == "evaluation-card")
	if err != nil {
		return fmt.Errorf("child %d: %w", child, err)
	}
	if err := verifyBundleBytes(manifest.body, descriptor.Digest, descriptor.Size); err != nil {
		return fmt.Errorf("child %d: %w", child, err)
	}
	if manifest.data["schemaVersion"] != float64(2) || manifest.data["mediaType"] != descriptor.MediaType {
		return fmt.Errorf("child %d is not an OCI schema 2 manifest", child)
	}
	if err := tc.fetchOCIManifestBlob(repository, manifest.data, role == "evaluation-card"); err != nil {
		return err
	}
	if role == "evaluation-card" {
		if !json.Valid(tc.ociArtifactBody) {
			return fmt.Errorf("evaluation card is not valid JSON")
		}
		// The card payload is validated further by the scenario's JSON-path assertions.
		layers, _ := manifest.data["layers"].([]interface{})
		if len(layers) != 1 {
			return fmt.Errorf("evaluation card must have exactly one layer")
		}
		layer, _ := layers[0].(map[string]interface{})
		digest, _ := layer["digest"].(string)
		size, ok := layer["size"].(float64)
		if !ok || strings.TrimSpace(digest) == "" {
			return fmt.Errorf("invalid evaluation card layer descriptor")
		}
		return verifyBundleBytes(tc.ociArtifactBody, digest, int64(size))
	}
	return nil
}
