package oci

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/eval-hub/eval-hub/pkg/ociclient"
)

type secretGetterFunc func(context.Context, string, string) ([]byte, error)

// GetDockerConfigJSON delegates secret lookup to the test callback.
func (f secretGetterFunc) GetDockerConfigJSON(ctx context.Context, namespace, secretName string) ([]byte, error) {
	return f(ctx, namespace, secretName)
}

// TestKubernetesCredentialResolverSelectsRegistryAndScopesLookup verifies tenant-scoped lookup and registry-specific credential selection.
func TestKubernetesCredentialResolverSelectsRegistryAndScopesLookup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	resolver := NewKubernetesCredentialResolver(secretGetterFunc(func(gotCtx context.Context, namespace, secretName string) ([]byte, error) {
		if gotCtx != ctx || namespace != "tenant-a" || secretName != "registry-secret" {
			t.Fatalf("unexpected secret lookup: context=%v namespace=%q secret=%q", gotCtx, namespace, secretName)
		}
		return []byte(`{"auths":{"quay.io":{"username":"quay-user","password":"quay-pass"},"registry.example":{"username":"other-user","password":"other-pass"}}}`), nil
	}))
	creds, err := resolver.Resolve(ctx, OCICredentialRequest{
		RegistryHost: "https://quay.io",
		Tenant:       "tenant-a",
		Connection:   &api.OCIConnectionConfig{Connection: "registry-secret"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if creds != (ociclient.Credentials{Username: "quay-user", Password: "quay-pass"}) {
		t.Fatal("credentials did not match the requested registry")
	}
}

// TestKubernetesCredentialResolverRejectsIncompleteLookup verifies missing connection or tenant data prevents secret lookup.
func TestKubernetesCredentialResolverRejectsIncompleteLookup(t *testing.T) {
	for _, req := range []OCICredentialRequest{
		{RegistryHost: "quay.io", Tenant: "tenant-a"},
		{RegistryHost: "quay.io", Tenant: "tenant-a", Connection: &api.OCIConnectionConfig{}},
		{RegistryHost: "quay.io", Connection: &api.OCIConnectionConfig{Connection: "secret"}},
	} {
		resolver := NewKubernetesCredentialResolver(secretGetterFunc(func(context.Context, string, string) ([]byte, error) {
			t.Fatal("incomplete requests must not fetch secrets")
			return nil, nil
		}))
		if _, err := resolver.Resolve(context.Background(), req); err == nil {
			t.Fatal("expected incomplete credential lookup to fail")
		}
	}
}

// TestKubernetesCredentialResolverPreservesLookupError verifies callers can identify the original secret lookup error.
func TestKubernetesCredentialResolverPreservesLookupError(t *testing.T) {
	want := errors.New("secret unavailable")
	resolver := NewKubernetesCredentialResolver(secretGetterFunc(func(context.Context, string, string) ([]byte, error) {
		return nil, want
	}))
	_, err := resolver.Resolve(context.Background(), OCICredentialRequest{
		RegistryHost: "quay.io", Tenant: "tenant-a", Connection: &api.OCIConnectionConfig{Connection: "secret"},
	})
	if !errors.Is(err, want) {
		t.Fatalf("lookup error = %v, want %v", err, want)
	}
}

// TestKubernetesCredentialResolverRejectsUnconfiguredGetter verifies nil receivers and getters fail with empty credentials.
func TestKubernetesCredentialResolverRejectsUnconfiguredGetter(t *testing.T) {
	for name, resolver := range map[string]OCICredentialResolver{
		"nil receiver": (*kubernetesCredentialResolver)(nil),
		"nil getter":   NewKubernetesCredentialResolver(nil),
	} {
		t.Run(name, func(t *testing.T) {
			creds, err := resolver.Resolve(context.Background(), OCICredentialRequest{})
			if err == nil || err.Error() != "oci secret getter is not configured" {
				t.Fatalf("expected unconfigured getter error, got %v", err)
			}
			if creds != (ociclient.Credentials{}) {
				t.Fatal("failed lookup must return empty credentials")
			}
		})
	}
}

// TestKubernetesCredentialResolverWrapsParseError verifies malformed secrets return empty credentials and preserve the JSON error.
func TestKubernetesCredentialResolverWrapsParseError(t *testing.T) {
	resolver := NewKubernetesCredentialResolver(secretGetterFunc(func(context.Context, string, string) ([]byte, error) {
		return []byte(`invalid-json`), nil
	}))
	creds, err := resolver.Resolve(context.Background(), OCICredentialRequest{
		RegistryHost: "quay.io", Tenant: "tenant-a", Connection: &api.OCIConnectionConfig{Connection: "secret"},
	})
	var syntaxError *json.SyntaxError
	if !errors.As(err, &syntaxError) {
		t.Fatalf("expected wrapped JSON syntax error, got %v", err)
	}
	if creds != (ociclient.Credentials{}) {
		t.Fatal("invalid secret must return empty credentials")
	}
}

// TestLocalCredentialResolver verifies anonymous access requires no tenant and ignores Kubernetes connections.
func TestLocalCredentialResolver(t *testing.T) {
	resolver := NewLocalCredentialResolver()
	creds, err := resolver.Resolve(context.Background(), OCICredentialRequest{RegistryHost: "http://localhost:5001"})
	if err != nil || creds != (ociclient.Credentials{}) {
		t.Fatal("local operations must resolve empty credentials without a tenant or secret")
	}
	for _, connection := range []*api.OCIConnectionConfig{{}, {Connection: "secret"}} {
		creds, err := resolver.Resolve(context.Background(), OCICredentialRequest{Connection: connection})
		if err != nil || creds != (ociclient.Credentials{}) {
			t.Fatal("local operations must ignore Kubernetes connections and resolve empty credentials")
		}
	}
}
