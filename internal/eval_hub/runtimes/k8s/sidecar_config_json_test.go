package k8s

import (
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
)

func TestSidecarForJobPodModelHTTPTimeoutPreservesSidecarModel(t *testing.T) {
	modelTimeout := 7 * time.Minute
	mlflowTimeout := 2 * time.Minute

	cfg := &config.Config{
		MLFlow: &config.MLFlowConfig{HTTPTimeout: mlflowTimeout},
		Sidecar: &config.SidecarConfig{
			Model: &config.SidecarModelConfig{
				HTTPTimeout: modelTimeout,
			},
		},
	}
	jc := &jobConfig{}

	out, err := sidecarForJobPod(cfg, jc, "http://upstream/v1")
	if err != nil {
		t.Fatalf("sidecarForJobPod: %v", err)
	}
	if out == nil || out.Model == nil {
		t.Fatalf("expected non-nil export.Model")
	}
	if out.Model.HTTPTimeout != modelTimeout {
		t.Fatalf("expected sidecar model http_timeout %v to be preserved, got %v", modelTimeout, out.Model.HTTPTimeout)
	}
}

func TestSidecarForJobPodModelHTTPTimeoutDoesNotCopyMLFlow(t *testing.T) {
	mlflowTimeout := 3 * time.Minute

	cfg := &config.Config{
		MLFlow: &config.MLFlowConfig{HTTPTimeout: mlflowTimeout},
		Sidecar: &config.SidecarConfig{
			Model: &config.SidecarModelConfig{},
		},
	}
	jc := &jobConfig{}

	out, err := sidecarForJobPod(cfg, jc, "http://upstream/v1")
	if err != nil {
		t.Fatalf("sidecarForJobPod: %v", err)
	}
	if out == nil || out.Model == nil {
		t.Fatalf("expected non-nil export.Model")
	}
	if out.Model.HTTPTimeout != 0 {
		t.Fatalf("expected model http_timeout unset (0) so sidecar uses default; mlflow timeout must not apply, got %v", out.Model.HTTPTimeout)
	}
}
