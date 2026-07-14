package server

import (
	"context"
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

func TestNewOCIPublisherFactoryLocalModeReturnsNoop(t *testing.T) {
	t.Parallel()

	factory := newOCIPublisherFactory(nil, &config.Config{
		Service: &config.ServiceConfig{LocalMode: true},
	})
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

func TestNewOCIPublisherFactoryNilConfigReturnsNoop(t *testing.T) {
	t.Parallel()

	factory := newOCIPublisherFactory(nil, nil)
	publisher, err := factory.NewPublisher(context.Background(), nil)
	if err != nil {
		t.Fatalf("NewPublisher() err = %v", err)
	}
	if err := publisher.PublishEvalCard(context.Background(), []byte(`{}`)); err != nil {
		t.Fatalf("PublishEvalCard() err = %v", err)
	}
}

func TestNewOCIPublisherFactoryReturnsErrorWhenHTTPClientInitFails(t *testing.T) {
	t.Parallel()

	if _, err := k8s.NewKubernetesHelper(); err != nil {
		t.Skipf("kubernetes client unavailable: %v", err)
	}

	badCA := filepath.Join(t.TempDir(), "bad-ca.crt")
	if err := os.WriteFile(badCA, []byte("not-a-cert"), 0o600); err != nil {
		t.Fatalf("WriteFile() err = %v", err)
	}

	factory := newOCIPublisherFactory(nil, &config.Config{
		Service: &config.ServiceConfig{LocalMode: false},
		Sidecar: &config.SidecarConfig{
			OCI: &config.SidecarOCIConfig{CACertPath: badCA},
		},
	})
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

func TestNewOCIPublisherFactoryClusterModeUsesRealFactory(t *testing.T) {
	t.Parallel()

	if _, err := k8s.NewKubernetesHelper(); err != nil {
		t.Skipf("kubernetes client unavailable: %v", err)
	}

	factory := newOCIPublisherFactory(nil, &config.Config{
		Service: &config.ServiceConfig{LocalMode: false},
	})
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
	if strings.Contains(err.Error(), "oci export unavailable: kubernetes client") {
		t.Fatalf("unexpected unavailable factory error: %v", err)
	}
}
