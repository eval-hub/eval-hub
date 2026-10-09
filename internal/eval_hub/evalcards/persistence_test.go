package evalcards

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
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
func (r *recordingTarget) Export(_ context.Context, _ *api.EvaluationJobResource, _ *cards.EvaluationCard) (ExportResult, error) {
	r.called = true
	return ExportResult{CardURL: r.cardURL}, nil
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
	if cardURL.CardURL != "https://example.com/card.json" {
		t.Fatalf("cardURL = %v", cardURL)
	}
	if !mlflowTarget.called {
		t.Fatal("expected mlflow target to be called")
	}
	if ociTarget.called {
		t.Fatal("expected oci target to be skipped")
	}
}

func TestMLflowTargetDisabledWithoutExperimentName(t *testing.T) {
	target := NewMLflowTarget(mlflowclient.NewClient("http://example.com"), nil, nil)
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
	target := NewMLflowTarget(mlflowclient.NewClient("http://example.com"), nil, nil)
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

	target := NewMLflowTarget(mlflowclient.NewClient(srv.URL), nil, nil)
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

	target := NewMLflowTarget(mlflowclient.NewClient(srv.URL), nil, nil)
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
	if cardURL.CardURL == "" {
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
	if target.Target() != TargetOCI {
		t.Fatalf("target = %q", target.Target())
	}
	cardURL, err := target.Export(context.Background(), job, &cards.EvaluationCard{})
	if err == nil {
		t.Fatalf("Export() err = %v", err)
	}
	if cardURL.CardURL != "" {
		t.Fatalf("cardURL = %v, want empty", cardURL)
	}
}

func TestNewManagerUsesDefaultTargets(t *testing.T) {
	manager := NewManager(nil, ManagerConfig{
		MLFlowClient: mlflowclient.NewClient("http://example.com"),
	})
	if manager == nil || len(manager.targets) != 2 {
		t.Fatalf("manager = %#v", manager)
	}
}

func TestManagerExportNilJobOrCard(t *testing.T) {
	manager := &Manager{}
	if url, err := manager.Export(context.Background(), nil, &cards.EvaluationCard{}); err != nil || url.CardURL != "" {
		t.Fatalf("nil job: url=%v err=%v", url, err)
	}
	job := &api.EvaluationJobResource{Resource: api.EvaluationResource{Resource: api.Resource{ID: "job-1"}}}
	if url, err := manager.Export(context.Background(), job, nil); err != nil || url.CardURL != "" {
		t.Fatalf("nil card: url=%v err=%v", url, err)
	}
}

type failingTarget struct {
	name Target
}

func (f *failingTarget) Target() Target { return f.name }
func (f *failingTarget) Enabled(_ *api.EvaluationJobResource) bool {
	return true
}
func (f *failingTarget) Export(_ context.Context, _ *api.EvaluationJobResource, _ *cards.EvaluationCard) (ExportResult, error) {
	return ExportResult{}, errors.New("export failed")
}

func TestManagerExportJoinsTargetErrors(t *testing.T) {
	manager := &Manager{targets: []ExportTarget{
		&failingTarget{name: TargetMLflow},
		&recordingTarget{name: TargetOCI, enabled: true, cardURL: "https://example.com/card.json"},
	}}
	job := &api.EvaluationJobResource{Resource: api.EvaluationResource{Resource: api.Resource{ID: "job-1"}}}
	cardURL, err := manager.Export(context.Background(), job, &cards.EvaluationCard{})
	if err == nil {
		t.Fatal("expected joined export error")
	}
	if cardURL.CardURL != "https://example.com/card.json" {
		t.Fatalf("cardURL = %v", cardURL)
	}
}

func TestMLflowTargetExportNilClient(t *testing.T) {
	target := NewMLflowTarget(nil, nil, nil)
	if target.Target() != TargetMLflow {
		t.Fatalf("target = %q", target.Target())
	}
	job := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{
			Resource:           api.Resource{ID: "job-1"},
			MLFlowExperimentID: "8",
		},
		EvaluationJobConfig: api.EvaluationJobConfig{
			Experiment: &api.ExperimentConfig{Name: "exp"},
		},
	}
	_, err := target.Export(context.Background(), job, &cards.EvaluationCard{CardVersion: cards.CardVersion})
	if err == nil {
		t.Fatal("expected error when mlflow client is nil")
	}
}

func TestMLflowTargetExportWithLoggerAndTenant(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/runs/create"):
			_ = json.NewEncoder(w).Encode(mlflowclient.CreateRunResponse{
				Run: mlflowclient.Run{Info: mlflowclient.RunInfo{RunID: "run-1"}},
			})
		case r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/mlflow-artifacts/artifacts/"):
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	target := NewMLflowTarget(mlflowclient.NewClient(srv.URL), nil, logger)
	job := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{
			Resource:           api.Resource{ID: "job-1", Tenant: "tenant-a"},
			MLFlowExperimentID: "8",
		},
		EvaluationJobConfig: api.EvaluationJobConfig{
			Name:       "demo",
			Experiment: &api.ExperimentConfig{Name: "exp"},
		},
	}
	cardURL, err := target.Export(context.Background(), job, &cards.EvaluationCard{CardVersion: cards.CardVersion})
	if err != nil || cardURL.CardURL == "" {
		t.Fatalf("Export() cardURL=%v err=%v", cardURL, err)
	}
}

type errOCIFactory struct{}

func (errOCIFactory) NewPublisher(_ context.Context, _ *api.EvaluationJobResource) (OCIPublisher, error) {
	return nil, errors.New("oci unavailable")
}

func TestOCITargetExportFactoryError(t *testing.T) {
	target := NewOCITarget(errOCIFactory{}, nil)
	job := &api.EvaluationJobResource{
		EvaluationJobConfig: api.EvaluationJobConfig{
			Exports: &api.EvaluationExports{OCI: &api.EvaluationExportsOCI{}},
		},
	}
	_, err := target.Export(context.Background(), job, &cards.EvaluationCard{CardVersion: cards.CardVersion})
	if err == nil {
		t.Fatal("expected error from oci publisher factory")
	}
}

type outcomeTarget struct {
	name   Target
	result ExportResult
	err    error
	calls  int
}

func (t *outcomeTarget) Target() Target                            { return t.name }
func (t *outcomeTarget) Enabled(_ *api.EvaluationJobResource) bool { return true }
func (t *outcomeTarget) Export(_ context.Context, _ *api.EvaluationJobResource, _ *cards.EvaluationCard) (ExportResult, error) {
	t.calls++
	return t.result, t.err
}

func TestManagerKeepsDestinationOutcomesIndependent(t *testing.T) {
	for _, ociFails := range []bool{true, false} {
		mlflow := &outcomeTarget{name: TargetMLflow, result: ExportResult{CardURL: "https://mlflow/card"}}
		oci := &outcomeTarget{name: TargetOCI, result: ExportResult{OCIArtifact: &api.OCIArtifactReference{OCIDigest: "sha256:manifest", OCIReference: "registry/repo@sha256:manifest"}}}
		if ociFails {
			oci.err = errors.New("registry failed")
			oci.result = ExportResult{}
		} else {
			mlflow.err = errors.New("MLflow failed")
		}
		manager := &Manager{targets: []ExportTarget{oci, mlflow}}
		result, err := manager.Export(context.Background(), &api.EvaluationJobResource{}, &cards.EvaluationCard{})
		if err == nil || mlflow.calls != 1 || oci.calls != 1 {
			t.Fatal("did not attempt both destinations")
		}
		if ociFails {
			if result.OCIError == nil || result.OCIArtifact != nil || result.CardURL == "" {
				t.Fatalf("result=%#v", result)
			}
		} else if result.OCIError != nil || result.OCIArtifact == nil {
			t.Fatalf("MLflow failure hid OCI success: %#v", result)
		}
	}
}

func TestOCITargetGenerationAndSerializationFailures(t *testing.T) {
	job := &api.EvaluationJobResource{Resource: api.EvaluationResource{Resource: api.Resource{ID: "job-1"}}}
	for _, tc := range []struct {
		card *cards.EvaluationCard
		code string
	}{
		{card: nil, code: "OCI_CARD_GENERATION_FAILED"},
		{card: &cards.EvaluationCard{}, code: "OCI_CARD_GENERATION_FAILED"},
		{card: cards.NewEvaluationCard(&api.EvaluationJobResource{Resource: job.Resource, Results: &api.EvaluationJobResults{Benchmarks: []api.BenchmarkResult{{Metrics: map[string]any{"invalid": make(chan int)}}}}}), code: "OCI_CARD_SERIALIZATION_FAILED"},
	} {
		_, err := NewOCITarget(errOCIFactory{}, nil).Export(context.Background(), job, tc.card)
		var exportErr *OCIExportError
		if !errors.As(err, &exportErr) || exportErr.Code != tc.code {
			t.Fatalf("error=%v, want %s", err, tc.code)
		}
	}
}

func TestManagerRejectsDiscardPublication(t *testing.T) {
	job := &api.EvaluationJobResource{Resource: api.EvaluationResource{Resource: api.Resource{ID: "job-1"}}, EvaluationJobConfig: api.EvaluationJobConfig{Exports: &api.EvaluationExports{OCI: &api.EvaluationExportsOCI{}}}}
	result, err := NewManager(nil, ManagerConfig{}).Export(context.Background(), job, cards.NewEvaluationCard(job))
	if err == nil || result.OCIError == nil || result.OCIArtifact != nil {
		t.Fatalf("discard publisher fabricated success: result=%#v, err=%v", result, err)
	}
}
