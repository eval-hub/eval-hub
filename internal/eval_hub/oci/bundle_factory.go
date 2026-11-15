package oci

import (
	"context"
	"net/http"

	"github.com/eval-hub/eval-hub/pkg/api"
)

// EvaluationBundlePublisher publishes an index from recorded job results and a published card.
type EvaluationBundlePublisher interface {
	Publish(context.Context, *api.EvaluationJobResource, *api.OCIArtifactReference) (*api.OCIArtifactReference, error)
}

// BundlePublisherFactory creates a publisher scoped to the job's registry and tenant.
type BundlePublisherFactory interface {
	NewPublisher(context.Context, *api.EvaluationJobResource) (EvaluationBundlePublisher, error)
}

type bundlePublisherFactory struct {
	resolver OCICredentialResolver
	client   *http.Client
}

// NewBundlePublisherFactory shares access configuration, never per-job credentials or clients.
func NewBundlePublisherFactory(resolver OCICredentialResolver, client *http.Client) BundlePublisherFactory {
	return &bundlePublisherFactory{resolver: resolver, client: client}
}

// NewPublisher resolves fresh job-scoped credentials before constructing the bundle publisher.
func (f *bundlePublisherFactory) NewPublisher(ctx context.Context, job *api.EvaluationJobResource) (EvaluationBundlePublisher, error) {
	client, err := NewJobRegistryClient(ctx, job, f.resolver, f.client)
	if err != nil {
		return nil, err
	}
	return NewBundlePublisher(client), nil
}
