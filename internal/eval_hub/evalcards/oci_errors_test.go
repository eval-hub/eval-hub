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
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestOCITargetKubernetesCredentialFailures(t *testing.T) {
	for _, tc := range []struct {
		name  string
		data  []byte
		err   error
		cause string
	}{
		{name: "missing Secret", err: k8serrors.NewNotFound(schema.GroupResource{Resource: "secrets"}, "connection"), cause: "Secret was not found"},
		{name: "forbidden Secret", err: k8serrors.NewForbidden(schema.GroupResource{Resource: "secrets"}, "connection", errors.New("password=private")), cause: "forbidden"},
		{name: "unusable credentials", data: []byte(`{"auths":{}}`), cause: "no auth for registry"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			factory := NewOCIPublisherFactory(oci.NewKubernetesCredentialResolver(stubDockerConfigSecretGetter{data: tc.data, err: tc.err}), http.DefaultClient)
			job := &api.EvaluationJobResource{Resource: api.EvaluationResource{Resource: api.Resource{ID: "job-1", Tenant: "tenant-a"}}, EvaluationJobConfig: api.EvaluationJobConfig{Exports: &api.EvaluationExports{OCI: &api.EvaluationExportsOCI{Coordinates: api.OCICoordinates{OCIHost: "quay.io", OCIRepository: "team/results"}, K8s: &api.OCIConnectionConfig{Connection: "connection"}}}}}
			_, err := NewOCITarget(factory, nil).Export(context.Background(), job, cards.NewEvaluationCard(job))
			var exportErr *OCIExportError
			if !errors.As(err, &exportErr) || exportErr.Code != "OCI_CARD_ACCESS_FAILED" || !strings.Contains(exportErr.Error(), tc.cause) || strings.Contains(exportErr.Error(), "private") {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestOCITargetPublicationFailureDoesNotExposeRegistryBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("token=private-registry-secret"))
	}))
	defer srv.Close()
	job := &api.EvaluationJobResource{Resource: api.EvaluationResource{Resource: api.Resource{ID: "job-1"}}, EvaluationJobConfig: api.EvaluationJobConfig{Exports: &api.EvaluationExports{OCI: &api.EvaluationExportsOCI{Coordinates: api.OCICoordinates{OCIHost: srv.URL, OCIRepository: "team/results"}}}}}
	target := NewOCITarget(NewOCIPublisherFactory(oci.NewLocalCredentialResolver(), srv.Client()), nil)
	result, err := target.Export(context.Background(), job, cards.NewEvaluationCard(job))
	var exportErr *OCIExportError
	if !errors.As(err, &exportErr) || exportErr.Code != "OCI_CARD_PUBLICATION_FAILED" || !strings.Contains(err.Error(), "403") || strings.Contains(err.Error(), "private") || result.OCIArtifact != nil {
		t.Fatalf("result=%#v, error=%v", result, err)
	}
}

func TestOCICauseDoesNotEchoArbitraryErrors(t *testing.T) {
	for _, err := range []error{errors.New("secret password=private"), errors.New("parse token realm url https://user:private@example.com/?token=private"), errors.New("failed with status 401: token=private")} {
		if strings.Contains(safeOCICause(err), "private") {
			t.Fatalf("unsafe cause: %s", safeOCICause(err))
		}
	}
}
