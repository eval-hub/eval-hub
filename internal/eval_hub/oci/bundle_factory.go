package oci

import (
	"context"
	"fmt"
	"net/http"

	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/eval-hub/eval-hub/pkg/ociclient"
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

// NewJobRegistryClient resolves current credentials and constructs a repository-scoped client.
// Card and bundle publishers use separate clients with the same HTTP/TLS and credential policy.
func NewJobRegistryClient(ctx context.Context, job *api.EvaluationJobResource, resolver OCICredentialResolver, httpClient *http.Client) (*ociclient.Client, error) {
	if job == nil || job.Exports == nil || job.Exports.OCI == nil {
		return nil, fmt.Errorf("oci export configuration is required")
	}
	if resolver == nil {
		return nil, fmt.Errorf("oci credential resolver is not configured")
	}
	if httpClient == nil {
		return nil, fmt.Errorf("oci http client is not configured")
	}
	export := job.Exports.OCI
	creds, err := resolver.Resolve(ctx, OCICredentialRequest{
		RegistryHost: export.Coordinates.OCIHost,
		Tenant:       job.Resource.Tenant.String(),
		Connection:   export.K8s,
	})
	if err != nil {
		return nil, fmt.Errorf("resolve OCI credentials: %w", err)
	}
	return ociclient.NewClient(export.Coordinates.OCIHost, export.Coordinates.OCIRepository, creds, httpClient)
}
