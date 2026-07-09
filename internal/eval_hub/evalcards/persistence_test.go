package evalcards

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/eval-hub/eval-hub/pkg/cards"
	"github.com/eval-hub/eval-hub/pkg/mlflowclient"
)

type recordingTarget struct {
	name    Target
	enabled bool
	called  bool
	cardURL string
}

func (r *recordingTarget) Target() Target { return r.name }
func (r *recordingTarget) Enabled(_ *api.EvaluationJobResource) bool {
	return r.enabled
}
func (r *recordingTarget) Export(_ context.Context, _ *api.EvaluationJobResource, _ *cards.EvaluationCard) (string, error) {
	r.called = true
	return r.cardURL, nil
}

func TestManagerExportEnabledTargetsOnly(t *testing.T) {
	mlflowTarget := &recordingTarget{name: TargetMLflow, enabled: true, cardURL: "https://example.com/card.json"}
	ociTarget := &recordingTarget{name: TargetOCI, enabled: false}
	manager := &Manager{targets: []ExportTarget{mlflowTarget, ociTarget}}

	job := &api.EvaluationJobResource{Resource: api.EvaluationResource{Resource: api.Resource{ID: "job-1"}}}
	card := &cards.EvaluationCard{CardVersion: cards.CardVersion}
	cardURL, err := manager.Export(context.Background(), job, card)
	if err != nil {
		t.Fatalf("Export() err = %v", err)
	}
	if cardURL != "https://example.com/card.json" {
		t.Fatalf("cardURL = %q", cardURL)
	}
	if !mlflowTarget.called {
		t.Fatal("expected mlflow target to be called")
	}
	if ociTarget.called {
		t.Fatal("expected oci target to be skipped")
	}
}

func TestMLflowTargetDisabledWithoutExperimentName(t *testing.T) {
	target := NewMLflowTarget(mlflowclient.NewClient("http://example.com"), nil)
	job := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{
			Resource:           api.Resource{ID: "job-1"},
			MLFlowExperimentID: "exp-1",
		},
	}
	if target.Enabled(job) {
		t.Fatal("expected mlflow target to be disabled without experiment name")
	}
}

func TestMLflowTargetDisabledWithoutExperimentID(t *testing.T) {
	target := NewMLflowTarget(mlflowclient.NewClient("http://example.com"), nil)
	job := &api.EvaluationJobResource{
		EvaluationJobConfig: api.EvaluationJobConfig{
			Experiment: &api.ExperimentConfig{Name: "exp"},
		},
	}
	if target.Enabled(job) {
		t.Fatal("expected mlflow target to be disabled without experiment id")
	}
}

func TestMLflowTargetExportWithoutArtifactLocation(t *testing.T) {
	t.Parallel()

	var uploadedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/runs/create"):
			_ = json.NewEncoder(w).Encode(mlflowclient.CreateRunResponse{
				Run: mlflowclient.Run{Info: mlflowclient.RunInfo{RunID: "run-1"}},
			})
		case r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/mlflow-artifacts/artifacts/"):
			uploadedPath = r.URL.Path
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	target := NewMLflowTarget(mlflowclient.NewClient(srv.URL), nil)
	job := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{
			Resource:           api.Resource{ID: "job-1"},
			MLFlowExperimentID: "8",
		},
		EvaluationJobConfig: api.EvaluationJobConfig{
			Name:       "demo",
			Experiment: &api.ExperimentConfig{Name: "exp"},
		},
	}
	_, err := target.Export(context.Background(), job, &cards.EvaluationCard{CardVersion: cards.CardVersion})
	if err != nil {
		t.Fatalf("Export() err = %v", err)
	}
	wantSuffix := "/mlflow-artifacts/artifacts/8/run-1/artifacts/evaluation-card.json"
	if !strings.HasSuffix(uploadedPath, wantSuffix) {
		t.Fatalf("uploaded path = %q, want suffix %q", uploadedPath, wantSuffix)
	}
}

func TestMLflowTargetExport(t *testing.T) {
	t.Parallel()

	var uploadedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/runs/create"):
			_ = json.NewEncoder(w).Encode(mlflowclient.CreateRunResponse{
				Run: mlflowclient.Run{Info: mlflowclient.RunInfo{RunID: "run-1"}},
			})
		case r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/mlflow-artifacts/artifacts/"):
			uploadedPath = r.URL.Path
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	target := NewMLflowTarget(mlflowclient.NewClient(srv.URL), nil)
	job := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{
			Resource:           api.Resource{ID: "job-1", Tenant: "tenant-a"},
			MLFlowExperimentID: "8",
		},
		EvaluationJobConfig: api.EvaluationJobConfig{
			Name: "demo",
			Experiment: &api.ExperimentConfig{
				Name:             "exp",
				ArtifactLocation: "/mlflow/artifacts/workspaces/sagar/8",
			},
		},
	}
	if !target.Enabled(job) {
		t.Fatal("expected mlflow target to be enabled")
	}
	cardURL, err := target.Export(context.Background(), job, &cards.EvaluationCard{CardVersion: cards.CardVersion})
	if err != nil {
		t.Fatalf("Export() err = %v", err)
	}
	if cardURL == "" {
		t.Fatal("expected non-empty card URL")
	}
	wantSuffix := "/mlflow-artifacts/artifacts/mlflow/artifacts/workspaces/sagar/8/8/run-1/artifacts/evaluation-card.json"
	if !strings.HasSuffix(uploadedPath, wantSuffix) {
		t.Fatalf("uploaded path = %q, want suffix %q", uploadedPath, wantSuffix)
	}
}

func TestOCITargetEnabled(t *testing.T) {
	target := NewOCITarget(nil, nil)
	job := &api.EvaluationJobResource{
		EvaluationJobConfig: api.EvaluationJobConfig{
			Exports: &api.EvaluationExports{OCI: &api.EvaluationExportsOCI{}},
		},
	}
	if !target.Enabled(job) {
		t.Fatal("expected oci target to be enabled")
	}
	cardURL, err := target.Export(context.Background(), job, &cards.EvaluationCard{})
	if err != nil {
		t.Fatalf("Export() err = %v", err)
	}
	if cardURL != "" {
		t.Fatalf("cardURL = %q, want empty", cardURL)
	}
}
