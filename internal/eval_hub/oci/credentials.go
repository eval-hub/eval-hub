package oci

import (
	"context"
	"fmt"

	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/eval-hub/eval-hub/pkg/ociclient"
)

// OCICredentialRequest identifies a registry and an optional tenant-scoped connection.
// It can be used by any OCI operation without requiring an evaluation job.
type OCICredentialRequest struct {
	RegistryHost string
	Tenant       string
	Connection   *api.OCIConnectionConfig
}

// OCICredentialResolver resolves credentials for an OCI registry operation.
type OCICredentialResolver interface {
	// Resolve returns credentials for the requested registry using the resolver's authentication mode.
	Resolve(context.Context, OCICredentialRequest) (ociclient.Credentials, error)
}

// DockerConfigSecretGetter reads kubernetes.io/dockerconfigjson secret payloads.
type DockerConfigSecretGetter interface {
	// GetDockerConfigJSON reads the named secret's Docker configuration from the given namespace.
	GetDockerConfigJSON(ctx context.Context, namespace, secretName string) ([]byte, error)
}

type kubernetesCredentialResolver struct {
	secretGetter DockerConfigSecretGetter
}

// NewKubernetesCredentialResolver resolves registry credentials from tenant secrets.
func NewKubernetesCredentialResolver(secretGetter DockerConfigSecretGetter) OCICredentialResolver {
	return &kubernetesCredentialResolver{secretGetter: secretGetter}
}

// Resolve validates the tenant and connection, then selects registry credentials from the tenant secret.
func (r *kubernetesCredentialResolver) Resolve(ctx context.Context, req OCICredentialRequest) (ociclient.Credentials, error) {
	if r == nil || r.secretGetter == nil {
		return ociclient.Credentials{}, fmt.Errorf("oci secret getter is not configured")
	}
	if req.Connection == nil || req.Connection.Connection == "" {
		return ociclient.Credentials{}, fmt.Errorf("oci k8s connection secret is required")
	}
	if req.Tenant == "" {
		return ociclient.Credentials{}, fmt.Errorf("tenant namespace is required for oci secret lookup")
	}
	secretData, err := r.secretGetter.GetDockerConfigJSON(ctx, req.Tenant, req.Connection.Connection)
	if err != nil {
		return ociclient.Credentials{}, err
	}
	creds, err := ociclient.ParseDockerConfigJSON(secretData, req.RegistryHost)
	if err != nil {
		return ociclient.Credentials{}, fmt.Errorf("parse oci credentials: %w", err)
	}
	return creds, nil
}

type localCredentialResolver struct{}

// NewLocalCredentialResolver supplies empty credentials for anonymous local OCI operations.
// Kubernetes connection settings are ignored in local mode.
func NewLocalCredentialResolver() OCICredentialResolver {
	return localCredentialResolver{}
}

// Resolve returns empty credentials for anonymous access, ignoring tenant and Kubernetes connection settings.
func (localCredentialResolver) Resolve(_ context.Context, _ OCICredentialRequest) (ociclient.Credentials, error) {
	return ociclient.Credentials{}, nil
}
