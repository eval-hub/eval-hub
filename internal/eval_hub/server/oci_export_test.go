package server

import (
	"context"
	"os"
	"path/filepath"
	"testing"

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
