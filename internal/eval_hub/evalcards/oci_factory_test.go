package evalcards

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/eval-hub/eval-hub/pkg/cards"
)

type stubDockerConfigSecretGetter struct {
	data []byte
	err  error
}

func (s stubDockerConfigSecretGetter) GetDockerConfigJSON(_ context.Context, _, _ string) ([]byte, error) {
	return s.data, s.err
}

func TestOCIPublisherFactoryNewPublisher(t *testing.T) {
	t.Parallel()

	var uploaded bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v2":
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodHead && strings.HasPrefix(r.URL.Path, "/v2/my-org/my-repo/blobs/"):
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodPost && r.URL.Path == "/v2/my-org/my-repo/blobs/uploads/":
			w.Header().Set("Location", "/v2/my-org/my-repo/blobs/uploads/upload-1")
			w.WriteHeader(http.StatusAccepted)
		case r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/blobs/uploads/"):
			w.WriteHeader(http.StatusCreated)
		case r.Method == http.MethodPut && r.URL.Path == "/v2/my-org/my-repo/manifests/eval-123-job-1":
			uploaded = true
			w.WriteHeader(http.StatusCreated)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	secretData := []byte(`{"auths":{"` + srv.URL + `":{"username":"user","password":"pass"}}}`)
	factory := NewOCIPublisherFactory(stubDockerConfigSecretGetter{data: secretData}, srv.Client())
	job := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{
			Resource: api.Resource{ID: "job-1", Tenant: "tenant-a"},
		},
		EvaluationJobConfig: api.EvaluationJobConfig{
			Exports: &api.EvaluationExports{
				OCI: &api.EvaluationExportsOCI{
					Coordinates: api.OCICoordinates{
						OCIHost:       srv.URL,
						OCIRepository: "my-org/my-repo",
						OCITag:        "eval-123",
					},
					K8s: &api.OCIConnectionConfig{Connection: "oci-pull-secret"},
				},
			},
		},
	}

	publisher, err := factory.NewPublisher(context.Background(), job)
	if err != nil {
		t.Fatalf("NewPublisher() err = %v", err)
	}
	defer func() { _ = publisher.Close() }()

	cardJSON, err := json.Marshal(&cards.EvaluationCard{CardVersion: cards.CardVersion})
	if err != nil {
		t.Fatalf("marshal card: %v", err)
	}
	if err := publisher.PublishEvalCard(context.Background(), cardJSON); err != nil {
		t.Fatalf("PublishEvalCard() err = %v", err)
	}
	if !uploaded {
		t.Fatal("expected manifest upload to registry")
	}
}

func TestOCIPublisherFactoryRequiresTenantSecret(t *testing.T) {
	factory := NewOCIPublisherFactory(stubDockerConfigSecretGetter{}, http.DefaultClient)
	job := &api.EvaluationJobResource{
		EvaluationJobConfig: api.EvaluationJobConfig{
			Exports: &api.EvaluationExports{
				OCI: &api.EvaluationExportsOCI{
					Coordinates: api.OCICoordinates{
						OCIHost:       "quay.io",
						OCIRepository: "org/repo",
					},
					K8s: &api.OCIConnectionConfig{Connection: "oci-secret"},
				},
			},
		},
	}
	if _, err := factory.NewPublisher(context.Background(), job); err == nil {
		t.Fatal("expected error without tenant namespace")
	}
}

func TestOCIPublisherFactoryDefaultsTagToJobID(t *testing.T) {
	t.Parallel()

	tag := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v2":
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodHead && strings.HasPrefix(r.URL.Path, "/v2/org/repo/blobs/"):
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodPost && r.URL.Path == "/v2/org/repo/blobs/uploads/":
			w.Header().Set("Location", "/v2/org/repo/blobs/uploads/upload-1")
			w.WriteHeader(http.StatusAccepted)
		case r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/blobs/uploads/"):
			w.WriteHeader(http.StatusCreated)
		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/v2/org/repo/manifests/"):
			tag = strings.TrimPrefix(r.URL.Path, "/v2/org/repo/manifests/")
			w.WriteHeader(http.StatusCreated)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	secretData := []byte(`{"auths":{"` + srv.URL + `":{"username":"user","password":"pass"}}}`)
	factory := NewOCIPublisherFactory(stubDockerConfigSecretGetter{data: secretData}, srv.Client())
	job := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{
			Resource: api.Resource{ID: "job-42", Tenant: "tenant-a"},
		},
		EvaluationJobConfig: api.EvaluationJobConfig{
			Exports: &api.EvaluationExports{
				OCI: &api.EvaluationExportsOCI{
					Coordinates: api.OCICoordinates{
						OCIHost:       srv.URL,
						OCIRepository: "org/repo",
					},
					K8s: &api.OCIConnectionConfig{Connection: "oci-secret"},
				},
			},
		},
	}
	publisher, err := factory.NewPublisher(context.Background(), job)
	if err != nil {
		t.Fatalf("NewPublisher() err = %v", err)
	}
	if err := publisher.PublishEvalCard(context.Background(), []byte(`{"card_version":"1.0"}`)); err != nil {
		t.Fatalf("PublishEvalCard() err = %v", err)
	}
	if tag != "evaluation-card-job-42" {
		t.Fatalf("tag = %q, want evaluation-card-job-42", tag)
	}
}

func TestOCIPublisherFactoryNewPublisherValidationErrors(t *testing.T) {
	t.Parallel()

	validJob := func() *api.EvaluationJobResource {
		return &api.EvaluationJobResource{
			Resource: api.EvaluationResource{
				Resource: api.Resource{ID: "job-1", Tenant: "tenant-a"},
			},
			EvaluationJobConfig: api.EvaluationJobConfig{
				Exports: &api.EvaluationExports{
					OCI: &api.EvaluationExportsOCI{
						Coordinates: api.OCICoordinates{
							OCIHost:       "quay.io",
							OCIRepository: "org/repo",
						},
						K8s: &api.OCIConnectionConfig{Connection: "oci-secret"},
					},
				},
			},
		}
	}

	factory := NewOCIPublisherFactory(stubDockerConfigSecretGetter{data: []byte(`{"auths":{}}`)}, http.DefaultClient)
	cases := []struct {
		name    string
		factory OCIPublisherFactory
		job     *api.EvaluationJobResource
	}{
		{name: "nil job", factory: factory, job: nil},
		{name: "missing exports", factory: factory, job: &api.EvaluationJobResource{}},
		{name: "missing oci export", factory: factory, job: &api.EvaluationJobResource{
			EvaluationJobConfig: api.EvaluationJobConfig{Exports: &api.EvaluationExports{}},
		}},
		{name: "missing k8s secret", factory: factory, job: &api.EvaluationJobResource{
			Resource: api.EvaluationResource{Resource: api.Resource{Tenant: "tenant-a"}},
			EvaluationJobConfig: api.EvaluationJobConfig{
				Exports: &api.EvaluationExports{OCI: &api.EvaluationExportsOCI{
					Coordinates: api.OCICoordinates{OCIHost: "quay.io", OCIRepository: "org/repo"},
				}},
			},
		}},
		{name: "nil secret getter", factory: NewOCIPublisherFactory(nil, http.DefaultClient), job: validJob()},
		{name: "nil http client", factory: NewOCIPublisherFactory(stubDockerConfigSecretGetter{}, nil), job: validJob()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := tc.factory.NewPublisher(context.Background(), tc.job); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestOCIPublisherFactorySecretGetterError(t *testing.T) {
	t.Parallel()

	factory := NewOCIPublisherFactory(
		stubDockerConfigSecretGetter{err: errors.New("secret unavailable")},
		http.DefaultClient,
	)
	job := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{Resource: api.Resource{ID: "job-1", Tenant: "tenant-a"}},
		EvaluationJobConfig: api.EvaluationJobConfig{
			Exports: &api.EvaluationExports{
				OCI: &api.EvaluationExportsOCI{
					Coordinates: api.OCICoordinates{OCIHost: "quay.io", OCIRepository: "org/repo"},
					K8s:         &api.OCIConnectionConfig{Connection: "oci-secret"},
				},
			},
		},
	}
	if _, err := factory.NewPublisher(context.Background(), job); err == nil {
		t.Fatal("expected secret getter error")
	}
}

func TestOCIPublisherPublishEvalCardNotConfigured(t *testing.T) {
	t.Parallel()

	publisher := &ociPublisher{}
	if err := publisher.PublishEvalCard(context.Background(), []byte(`{"card_version":"1.0"}`)); err == nil {
		t.Fatal("expected error for unconfigured publisher")
	}
}

func TestNoopOCIPublisherFactory(t *testing.T) {
	t.Parallel()

	factory := NewNoopOCIPublisherFactory()
	publisher, err := factory.NewPublisher(context.Background(), nil)
	if err != nil {
		t.Fatalf("NewPublisher() err = %v", err)
	}
	if err := publisher.PublishEvalCard(context.Background(), []byte(`{"card_version":"1.0"}`)); err != nil {
		t.Fatalf("PublishEvalCard() err = %v", err)
	}
	if err := publisher.Close(); err != nil {
		t.Fatalf("Close() err = %v", err)
	}
}

func TestOCIPublisherFactoryInvalidCredentials(t *testing.T) {
	t.Parallel()

	factory := NewOCIPublisherFactory(
		stubDockerConfigSecretGetter{data: []byte(`{"auths":{}}`)},
		http.DefaultClient,
	)
	job := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{Resource: api.Resource{ID: "job-1", Tenant: "tenant-a"}},
		EvaluationJobConfig: api.EvaluationJobConfig{
			Exports: &api.EvaluationExports{
				OCI: &api.EvaluationExportsOCI{
					Coordinates: api.OCICoordinates{
						OCIHost:       "quay.io",
						OCIRepository: "org/repo",
					},
					K8s: &api.OCIConnectionConfig{Connection: "oci-secret"},
				},
			},
		},
	}
	if _, err := factory.NewPublisher(context.Background(), job); err == nil {
		t.Fatal("expected credential parse error")
	}
}

func TestOCIPublisherFactoryInvalidRepository(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/v2" {
			w.WriteHeader(http.StatusOK)
		}
	}))
	t.Cleanup(srv.Close)

	secretData := []byte(`{"auths":{"` + srv.URL + `":{"username":"user","password":"pass"}}}`)
	factory := NewOCIPublisherFactory(stubDockerConfigSecretGetter{data: secretData}, srv.Client())
	job := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{Resource: api.Resource{ID: "job-1", Tenant: "tenant-a"}},
		EvaluationJobConfig: api.EvaluationJobConfig{
			Exports: &api.EvaluationExports{
				OCI: &api.EvaluationExportsOCI{
					Coordinates: api.OCICoordinates{
						OCIHost:       srv.URL,
						OCIRepository: "",
					},
					K8s: &api.OCIConnectionConfig{Connection: "oci-secret"},
				},
			},
		},
	}
	if _, err := factory.NewPublisher(context.Background(), job); err == nil {
		t.Fatal("expected repository validation error")
	}
}
