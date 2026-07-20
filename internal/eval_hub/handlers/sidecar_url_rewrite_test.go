package handlers

import (
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
	"github.com/eval-hub/eval-hub/pkg/api"
)

func TestSidecarBaseURL(t *testing.T) {
	t.Parallel()
	h := &Handlers{}
	if got := h.sidecarBaseURL(); got != "http://localhost:8080" {
		t.Fatalf("default sidecarBaseURL = %q", got)
	}

	h.serviceConfig = &config.Config{Sidecar: &config.SidecarConfig{Port: 9090}}
	if got := h.sidecarBaseURL(); got != "http://localhost:9090" {
		t.Fatalf("port-derived sidecarBaseURL = %q", got)
	}

	h.serviceConfig.Sidecar.BaseURL = "http://127.0.0.1:8080"
	if got := h.sidecarBaseURL(); got != "http://127.0.0.1:8080" {
		t.Fatalf("configured sidecarBaseURL = %q", got)
	}
}

func TestSidecarURLTargets(t *testing.T) {
	t.Parallel()
	h := &Handlers{
		serviceConfig: &config.Config{
			MLFlow: &config.MLFlowConfig{TrackingURI: "https://mlflow.example"},
		},
	}
	job := &api.EvaluationJobResource{
		EvaluationJobConfig: api.EvaluationJobConfig{
			Model: api.ModelRef{URL: "https://model.example/v1", Name: "m"},
			Exports: &api.EvaluationExports{
				OCI: &api.EvaluationExportsOCI{
					Coordinates: api.OCICoordinates{
						OCIHost:       "quay.io",
						OCIRepository: "org/repo",
					},
				},
			},
		},
	}
	got := h.sidecarURLTargets(job)
	if got.MLFlow != "https://mlflow.example" {
		t.Fatalf("MLFlow = %q", got.MLFlow)
	}
	if got.Model != "https://model.example/v1" {
		t.Fatalf("Model = %q", got.Model)
	}
	if got.OCI != "https://quay.io" {
		t.Fatalf("OCI = %q", got.OCI)
	}
	if got.OCIRepository != "org/repo" {
		t.Fatalf("OCIRepository = %q", got.OCIRepository)
	}
}

func TestNormalizeRegistryURL(t *testing.T) {
	t.Parallel()
	if got := normalizeRegistryURL("quay.io"); got != "https://quay.io" {
		t.Fatalf("got %q", got)
	}
	if got := normalizeRegistryURL("http://registry:5000"); got != "http://registry:5000" {
		t.Fatalf("got %q", got)
	}
	if got := normalizeRegistryURL(""); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestEvalHubServiceURLRequiresInstanceName(t *testing.T) {
	t.Setenv(evalHubInstanceNameEnv, "")
	if got := evalHubServiceURL("tenant-ns"); got != "" {
		t.Fatalf("expected empty without instance name, got %q", got)
	}

	t.Setenv(evalHubInstanceNameEnv, "evalhub")
	got := evalHubServiceURL("tenant-ns")
	want := "https://evalhub.tenant-ns.svc.cluster.local:8443"
	if got != want {
		t.Fatalf("evalHubServiceURL = %q, want %q", got, want)
	}
}
