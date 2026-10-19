package oci

import (
	"context"
	"net/http"
	"testing"

	"github.com/eval-hub/eval-hub/pkg/api"
)

// TestNewJobRegistryClientRequiresAccessConfiguration rejects missing job, resolver, or HTTP client before publication.
func TestNewJobRegistryClientRequiresAccessConfiguration(t *testing.T) {
	job := completedJob()
	for _, tc := range []struct {
		job      *api.EvaluationJobResource
		resolver OCICredentialResolver
		client   *http.Client
	}{
		{nil, NewLocalCredentialResolver(), http.DefaultClient},
		{job, nil, http.DefaultClient},
		{job, NewLocalCredentialResolver(), nil},
	} {
		if _, err := NewJobRegistryClient(context.Background(), tc.job, tc.resolver, tc.client); err == nil {
			t.Fatal("missing access configuration accepted")
		}
	}
}
