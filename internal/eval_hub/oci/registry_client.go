package oci

import (
	"context"
	"fmt"
	"net/http"

	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/eval-hub/eval-hub/pkg/ociclient"
)

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
