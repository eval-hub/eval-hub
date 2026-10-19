package evalcards

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/oci"
	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/eval-hub/eval-hub/pkg/cards"
	"github.com/eval-hub/eval-hub/pkg/ociclient"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestOCITargetKubernetesCredentialFailures(t *testing.T) {
	for _, tc := range []struct {
		name   string
		data   []byte
		err    error
		reason string
	}{
		{name: "missing Secret", err: k8serrors.NewNotFound(schema.GroupResource{Resource: "secrets"}, "connection"), reason: `secrets "connection" not found`},
		{name: "forbidden Secret", err: k8serrors.NewForbidden(schema.GroupResource{Resource: "secrets"}, "connection", errors.New("access denied")), reason: "forbidden"},
		{name: "missing Docker config", err: errors.New("docker configuration is missing"), reason: "docker configuration is missing"},
		{name: "unusable credentials", data: []byte(`{"auths":{}}`), reason: "registry credentials are missing"},
		{name: "malformed credentials", data: []byte(`{"auths":`), reason: "registry credentials are invalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			factory := NewOCIPublisherFactory(oci.NewKubernetesCredentialResolver(stubDockerConfigSecretGetter{data: tc.data, err: tc.err}), http.DefaultClient)
			job := &api.EvaluationJobResource{Resource: api.EvaluationResource{Resource: api.Resource{ID: "job-1", Tenant: "tenant-a"}}, EvaluationJobConfig: api.EvaluationJobConfig{Exports: &api.EvaluationExports{OCI: &api.EvaluationExportsOCI{Coordinates: api.OCICoordinates{OCIHost: "quay.io", OCIRepository: "team/results"}, K8s: &api.OCIConnectionConfig{Connection: "connection"}}}}}
			_, err := NewOCITarget(factory, nil).Export(context.Background(), job, cards.NewEvaluationCard(job))
			var exportErr *oci.OperationError
			if !errors.As(err, &exportErr) || exportErr.Operation != "initialize OCI publisher and resolve registry credentials" || !strings.Contains(exportErr.Error(), tc.reason) {
				t.Fatalf("error=%v", err)
			}
			if tc.err != nil && !errors.Is(err, tc.err) {
				t.Fatalf("credential error identity lost: %v", err)
			}
		})
	}
}

func TestOCITargetPublicationFailurePreservesRegistryResponse(t *testing.T) {
	const message = `{"errors":[{"code":"DENIED","message":"repository access denied"}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(message))
	}))
	defer srv.Close()
	job := &api.EvaluationJobResource{Resource: api.EvaluationResource{Resource: api.Resource{ID: "job-1"}}, EvaluationJobConfig: api.EvaluationJobConfig{Exports: &api.EvaluationExports{OCI: &api.EvaluationExportsOCI{Coordinates: api.OCICoordinates{OCIHost: srv.URL, OCIRepository: "team/results"}}}}}
	target := NewOCITarget(NewOCIPublisherFactory(oci.NewLocalCredentialResolver(), srv.Client()), nil)
	result, err := target.Export(context.Background(), job, cards.NewEvaluationCard(job))
	var exportErr *oci.OperationError
	if !errors.As(err, &exportErr) || exportErr.Operation != "publish evaluation card to OCI" || !strings.Contains(err.Error(), message) || result != nil {
		t.Fatalf("result=%#v, error=%v", result, err)
	}
	var httpErr *ociclient.RegistryHTTPError
	if !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusForbidden || httpErr.Message != message || httpErr.Operation != "put manifest" {
		t.Fatalf("registry response lost: %v", err)
	}
}
