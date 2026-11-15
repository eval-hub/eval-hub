package evalcards

import (
	"context"
	"fmt"
	"net/http"

	"github.com/eval-hub/eval-hub/internal/eval_hub/oci"
	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/eval-hub/eval-hub/pkg/ociclient"
)

type ociPublisherFactory struct {
	credentialResolver oci.OCICredentialResolver
	httpClient         *http.Client
}

// NewOCIPublisherFactory creates per-job OCI publishers using the supplied credential resolver.
// Each evaluation job may target a different registry/repository and connection, so publishers are
// constructed per export rather than shared globally.
func NewOCIPublisherFactory(resolver oci.OCICredentialResolver, httpClient *http.Client) OCIPublisherFactory {
	return &ociPublisherFactory{
		credentialResolver: resolver,
		httpClient:         httpClient,
	}
}

// NewPublisher resolves credentials and builds a client tagged for this evaluation job.
func (f *ociPublisherFactory) NewPublisher(ctx context.Context, job *api.EvaluationJobResource) (OCIPublisher, error) {
	if f == nil {
		return nil, fmt.Errorf("oci publisher factory is not configured")
	}
	client, err := oci.NewJobRegistryClient(ctx, job, f.credentialResolver, f.httpClient)
	if err != nil {
		return nil, err
	}
	return &ociPublisher{
		client:      client,
		jobID:       job.Resource.ID,
		annotations: job.Exports.OCI.Coordinates.Annotations,
	}, nil
}

type ociPublisher struct {
	client      *ociclient.Client
	jobID       string
	annotations map[string]string
}

// PublishEvalCard publishes the card and returns the identity of its accepted OCI manifest.
func (p *ociPublisher) PublishEvalCard(ctx context.Context, cardJSON []byte) (*api.OCIArtifactReference, error) {
	if p == nil || p.client == nil {
		return nil, fmt.Errorf("oci publisher is not configured")
	}
	return p.client.PushEvaluationCard(ctx, p.jobID, cardJSON, p.annotations)
}

// Close releases per-job publisher resources. The current implementation has no persistent state.
func (p *ociPublisher) Close() error {
	return nil
}
