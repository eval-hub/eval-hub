package oci

import (
	"context"
	"fmt"
	"slices"
	"strconv"

	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/eval-hub/eval-hub/pkg/ociclient"
)

// RegistryClient reads verified child descriptors and publishes indexes in one
// configured repository. *ociclient.Client implements this for HTTP and HTTPS,
// with anonymous access or registry authentication as configured by the caller.
type RegistryClient interface {
	// GetManifest verifies a manifest's bytes and metadata before returning its descriptor.
	GetManifest(context.Context, string) (ociclient.Descriptor, error)
	// PushIndex publishes the index under the final tag and returns its digest-qualified identity.
	PushIndex(context.Context, string, ociclient.Index) (*api.OCIArtifactReference, error)
}

// BundlePublisher builds bundles using an injected, per-job registry client. The caller
// must configure it with the job's OCI registry/repository and credentials.
type BundlePublisher struct{ client RegistryClient }

// NewBundlePublisher creates a publisher without resolving credentials or doing I/O.
func NewBundlePublisher(client RegistryClient) *BundlePublisher {
	return &BundlePublisher{client: client}
}

// EligibleForBundle reports whether the stored outcome and export settings allow bundling.
func EligibleForBundle(job *api.EvaluationJobResource) bool {
	return job != nil && job.Exports != nil && job.Exports.OCI != nil && job.Status != nil &&
		(job.Status.State == api.OverallStateCompleted || job.Status.State == api.OverallStatePartiallyFailed)
}

// Publish returns nil, nil for an ineligible job. For eligible jobs, every
// recorded completed result and the card must verify before any index PUT.
// Membership comes solely from recorded results and statuses; no definitions or
// collections are resolved. Neither the job nor its results are modified.
func (p *BundlePublisher) Publish(ctx context.Context, job *api.EvaluationJobResource, card *api.OCIArtifactReference) (*api.OCIArtifactReference, error) {
	if !EligibleForBundle(job) {
		return nil, nil
	}
	if p == nil || p.client == nil {
		return nil, fmt.Errorf("bundle registry client is required")
	}
	tag, err := ociclient.EvaluationBundleManifestTag(job.Resource.ID, job.Exports.OCI.Coordinates.OCITag)
	if err != nil {
		return nil, fmt.Errorf("bundle tag: %w", err)
	}
	if card == nil {
		return nil, fmt.Errorf("published evaluation card is required")
	}
	if err := ociclient.ValidateSHA256Digest(card.OCIDigest); err != nil {
		return nil, fmt.Errorf("evaluation card: %w", err)
	}
	selected, skipped, err := extractBenchmarksForIndex(job)
	if err != nil {
		return nil, err
	}
	cardDescriptor, err := p.client.GetManifest(ctx, card.OCIDigest)
	if err != nil {
		return nil, fmt.Errorf("verify evaluation card manifest: %w", err)
	}
	cardDescriptor.Annotations = map[string]string{"artifact_role": "evaluation-card"}
	descriptors := make([]ociclient.Descriptor, 0, len(selected)+1)
	descriptors = append(descriptors, cardDescriptor)
	for _, result := range selected {
		d, err := p.client.GetManifest(ctx, result.digest)
		if err != nil {
			return nil, fmt.Errorf("verify benchmark %d manifest: %w", result.index, err)
		}
		d.Annotations = map[string]string{
			"artifact_role":   "benchmark",
			"benchmark_index": strconv.Itoa(result.index),
			"benchmark_id":    result.id,
			"provider_id":     result.providerID,
		}
		descriptors = append(descriptors, d)
	}
	annotations := make(map[string]string, len(job.Exports.OCI.Coordinates.Annotations)+4)
	for key, value := range job.Exports.OCI.Coordinates.Annotations {
		annotations[key] = value
	}
	annotations["evaluation_job_id"] = job.Resource.ID
	annotations["evaluation_state"] = string(job.Status.State)
	annotations["included_benchmark_count"] = strconv.Itoa(len(selected))
	annotations["skipped_benchmark_count"] = strconv.Itoa(skipped)
	ref, err := p.client.PushIndex(ctx, tag, ociclient.Index{
		SchemaVersion: 2, MediaType: ociclient.MediaTypeImageIndex,
		Manifests: descriptors, Annotations: annotations,
	})
	if err != nil {
		return nil, fmt.Errorf("publish evaluation bundle: %w", err)
	}
	return ref, nil
}

type indexBenchmark struct {
	index      int
	id         string
	providerID string
	digest     string
}

// extractBenchmarksForIndex returns completed benchmark identities and validated
// digests in original index order and counts
// failed/cancelled statuses, including those without results. Each result needs
// a terminal status; completed results also need a usable artifact digest.
func extractBenchmarksForIndex(job *api.EvaluationJobResource) ([]indexBenchmark, int, error) {
	statuses := make(map[int]api.State, len(job.Status.Benchmarks))
	skipped := 0
	for _, status := range job.Status.Benchmarks {
		statuses[status.BenchmarkIndex] = status.Status
		if status.Status == api.StateFailed || status.Status == api.StateCancelled {
			skipped++
		}
	}
	var selected []indexBenchmark
	if job.Results != nil {
		for _, result := range job.Results.Benchmarks {
			status, ok := statuses[result.BenchmarkIndex]
			if !ok || !api.IsBenchmarkTerminalState(status) {
				return nil, 0, fmt.Errorf("benchmark %d has no recorded terminal status", result.BenchmarkIndex)
			}
			if status != api.StateCompleted {
				continue
			}
			digest, ok := result.Artifacts["oci_digest"].(string)
			if !ok {
				return nil, 0, fmt.Errorf("benchmark %d requires a string artifacts.oci_digest", result.BenchmarkIndex)
			}
			if err := ociclient.ValidateSHA256Digest(digest); err != nil {
				return nil, 0, fmt.Errorf("benchmark %d: %w", result.BenchmarkIndex, err)
			}
			selected = append(selected, indexBenchmark{
				index:      result.BenchmarkIndex,
				id:         result.ID,
				providerID: result.ProviderID,
				digest:     digest,
			})
		}
	}
	slices.SortFunc(selected, func(a, b indexBenchmark) int {
		// Compare without subtraction so stored indices cannot overflow.
		if a.index < b.index {
			return -1
		}
		if a.index > b.index {
			return 1
		}
		return 0
	})
	return selected, skipped, nil
}
