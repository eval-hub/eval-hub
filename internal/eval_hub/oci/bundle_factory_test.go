package oci

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/eval-hub/eval-hub/pkg/ociclient"
)

type recordingCredentialResolver struct {
	requests []OCICredentialRequest
	err      error
}

// Resolve records credential scopes and returns a configurable registry failure.
func (r *recordingCredentialResolver) Resolve(_ context.Context, req OCICredentialRequest) (ociclient.Credentials, error) {
	r.requests = append(r.requests, req)
	return ociclient.Credentials{}, r.err
}

// TestBundleFactoryResolvesEachJob checks tenant isolation, fresh resolution, and preservation of typed errors.
func TestBundleFactoryResolvesEachJob(t *testing.T) {
	resolver := &recordingCredentialResolver{}
	factory := NewBundlePublisherFactory(resolver, http.DefaultClient)
	for _, tenant := range []string{"tenant-a", "tenant-b"} {
		job := &api.EvaluationJobResource{
			Resource: api.EvaluationResource{Resource: api.Resource{Tenant: api.Tenant(tenant)}},
			EvaluationJobConfig: api.EvaluationJobConfig{Exports: &api.EvaluationExports{OCI: &api.EvaluationExportsOCI{
				Coordinates: api.OCICoordinates{OCIHost: "quay.io", OCIRepository: tenant + "/results"},
				K8s:         &api.OCIConnectionConfig{Connection: tenant + "-secret"},
			}}},
		}
		if _, err := factory.NewPublisher(context.Background(), job); err != nil {
			t.Fatal(err)
		}
		got := resolver.requests[len(resolver.requests)-1]
		if got.Tenant != tenant || got.RegistryHost != "quay.io" || got.Connection.Connection != tenant+"-secret" {
			t.Fatalf("wrong access scope: %+v", got)
		}
	}
	if len(resolver.requests) != 2 {
		t.Fatal("credentials were reused between jobs")
	}
	cause := &ociclient.RegistryHTTPError{StatusCode: http.StatusForbidden, Message: "denied"}
	resolver.err = cause
	job := completedJob()
	_, err := factory.NewPublisher(context.Background(), job)
	var typed *ociclient.RegistryHTTPError
	if !errors.As(err, &typed) || typed != cause {
		t.Fatalf("lost typed cause: %v", err)
	}
}
