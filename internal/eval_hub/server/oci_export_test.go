package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
	"github.com/eval-hub/eval-hub/internal/eval_hub/runtimes/k8s"
	"github.com/eval-hub/eval-hub/pkg/api"
)

// TestNewOCIPublisherFactoryLocalModeUsesAnonymousPublisher verifies local mode creates publishers without tenant secrets and validates exports.
func TestNewOCIPublisherFactoryLocalModeUsesAnonymousPublisher(t *testing.T) {
	t.Parallel()
	factory, cleanup := newOCIPublisherFactory(nil, &config.Config{
		Service: &config.ServiceConfig{LocalMode: true},
	})
	defer cleanup()
	job := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{Resource: api.Resource{ID: "job-local"}},
		EvaluationJobConfig: api.EvaluationJobConfig{Exports: &api.EvaluationExports{
			OCI: &api.EvaluationExportsOCI{Coordinates: api.OCICoordinates{
				OCIHost: "http://localhost:5001", OCIRepository: "myorg/eval-results",
			}},
		}},
	}
	publisher, err := factory.NewPublisher(context.Background(), job)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = publisher.Close() }()
	if _, err := factory.NewPublisher(context.Background(), nil); err == nil {
		t.Fatal("expected missing export configuration to fail")
	}
}

func TestNewOCIPublisherFactoryNilConfigReturnsNoop(t *testing.T) {
	t.Parallel()

	factory, cleanup := newOCIPublisherFactory(nil, nil)
	defer cleanup()
	publisher, err := factory.NewPublisher(context.Background(), nil)
	if err != nil {
		t.Fatalf("NewPublisher() err = %v", err)
	}
	if err := publisher.PublishEvalCard(context.Background(), []byte(`{}`)); err != nil {
		t.Fatalf("PublishEvalCard() err = %v", err)
	}
}

// TestNewOCIPublisherFactoryReturnsErrorWhenHTTPClientInitFails verifies invalid cluster CA configuration surfaces during publisher creation.
func TestNewOCIPublisherFactoryReturnsErrorWhenHTTPClientInitFails(t *testing.T) {
	configureOCITestKubernetesAPI(t)

	badCA := filepath.Join(t.TempDir(), "bad-ca.crt")
	if err := os.WriteFile(badCA, []byte("not-a-cert"), 0o600); err != nil {
		t.Fatalf("WriteFile() err = %v", err)
	}

	factory, cleanup := newOCIPublisherFactory(nil, &config.Config{
		Service: &config.ServiceConfig{LocalMode: false},
		Sidecar: &config.SidecarConfig{
			OCI: &config.SidecarOCIConfig{CACertPath: badCA},
		},
	})
	defer cleanup()
	_, err := factory.NewPublisher(context.Background(), &api.EvaluationJobResource{
		EvaluationJobConfig: api.EvaluationJobConfig{
			Exports: &api.EvaluationExports{OCI: &api.EvaluationExportsOCI{}},
		},
	})
	if err == nil {
		t.Fatal("expected OCI initialization error from NewPublisher")
	}
}

func TestKubernetesDockerConfigSecretGetter(t *testing.T) {
	t.Parallel()

	t.Run("not configured", func(t *testing.T) {
		t.Parallel()
		getter := &kubernetesDockerConfigSecretGetter{}
		if _, err := getter.GetDockerConfigJSON(context.Background(), "tenant-a", "oci-secret"); err == nil {
			t.Fatal("expected error for unconfigured getter")
		}
	})

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "oci-secret", Namespace: "tenant-a"},
			Data: map[string][]byte{
				".dockerconfigjson": []byte(`{"auths":{"https://quay.io":{"username":"u","password":"p"}}}`),
			},
		}
		helper := k8s.NewKubernetesHelperWithClientset(fake.NewClientset(secret))
		getter := newKubernetesDockerConfigSecretGetter(helper)
		raw, err := getter.GetDockerConfigJSON(context.Background(), "tenant-a", "oci-secret")
		if err != nil {
			t.Fatalf("GetDockerConfigJSON() err = %v", err)
		}
		if len(raw) == 0 {
			t.Fatal("expected dockerconfigjson payload")
		}
	})

	t.Run("missing secret", func(t *testing.T) {
		t.Parallel()
		helper := k8s.NewKubernetesHelperWithClientset(fake.NewClientset())
		getter := newKubernetesDockerConfigSecretGetter(helper)
		if _, err := getter.GetDockerConfigJSON(context.Background(), "tenant-a", "missing"); err == nil {
			t.Fatal("expected secret lookup error")
		}
	})
}

// TestNewOCIPublisherFactoryClusterModeUsesRealFactory verifies cluster publishing attempts the configured tenant secret lookup.
func TestNewOCIPublisherFactoryClusterModeUsesRealFactory(t *testing.T) {
	configureOCITestKubernetesAPI(t)

	factory, cleanup := newOCIPublisherFactory(nil, &config.Config{
		Service: &config.ServiceConfig{LocalMode: false},
	})
	defer cleanup()
	_, err := factory.NewPublisher(context.Background(), &api.EvaluationJobResource{
		Resource: api.EvaluationResource{Resource: api.Resource{ID: "job-1", Tenant: "tenant-a"}},
		EvaluationJobConfig: api.EvaluationJobConfig{
			Exports: &api.EvaluationExports{
				OCI: &api.EvaluationExportsOCI{
					Coordinates: api.OCICoordinates{OCIHost: "quay.io", OCIRepository: "org/repo"},
					K8s:         &api.OCIConnectionConfig{Connection: "oci-secret"},
				},
			},
		},
	})
	if err == nil {
		t.Fatal("expected publisher creation error without tenant secret")
	}
	if !strings.Contains(err.Error(), `get secret "oci-secret" in namespace "tenant-a"`) {
		t.Fatalf("expected tenant secret lookup error from real factory, got: %v", err)
	}
}

// configureOCITestKubernetesAPI installs a temporary kubeconfig and API stub so factory tests need no developer cluster.
func configureOCITestKubernetesAPI(t *testing.T) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/namespaces/tenant-a/secrets/oci-secret" {
			t.Errorf("unexpected Kubernetes request: %s", r.URL.Path)
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)
	kubeconfig := filepath.Join(t.TempDir(), "kubeconfig")
	data := fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
- name: test
  cluster:
    server: %s
contexts:
- name: test
  context:
    cluster: test
    user: test
current-context: test
users:
- name: test
  user: {}
`, server.URL)
	if err := os.WriteFile(kubeconfig, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KUBECONFIG", kubeconfig)
	t.Setenv("KUBERNETES_SERVICE_HOST", "")
	t.Setenv("KUBERNETES_SERVICE_PORT", "")
}

// TestLocalOCIPublisherFactoryHTTPClientInitFailure verifies invalid local CA configuration surfaces during publisher creation.
func TestLocalOCIPublisherFactoryHTTPClientInitFailure(t *testing.T) {
	badCA := filepath.Join(t.TempDir(), "bad-ca.crt")
	if err := os.WriteFile(badCA, []byte("invalid"), 0o600); err != nil {
		t.Fatal(err)
	}
	factory, cleanup := newOCIPublisherFactory(nil, &config.Config{
		Service: &config.ServiceConfig{LocalMode: true},
		Sidecar: &config.SidecarConfig{OCI: &config.SidecarOCIConfig{CACertPath: badCA}},
	})
	defer cleanup()
	if _, err := factory.NewPublisher(context.Background(), nil); err == nil {
		t.Fatal("expected invalid local CA configuration to fail")
	}
}
