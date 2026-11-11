package handlers

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/evalcards"
	"github.com/eval-hub/eval-hub/internal/eval_hub/postprocessing"
	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/eval-hub/eval-hub/pkg/cards"
)

type stubResultsExporter struct {
	cardURL string
	err     error
	card    *cards.EvaluationCard
}

func (s *stubResultsExporter) Export(_ context.Context, _ *api.EvaluationJobResource, card *cards.EvaluationCard) (evalcards.ExportResult, error) {
	s.card = card
	return evalcards.ExportResult{CardURL: s.cardURL}, s.err
}

func TestExportEvaluationResultsAsyncEligibleStates(t *testing.T) {
	for _, tc := range []struct {
		state api.OverallState
		want  bool
	}{
		{api.OverallStateCompleted, true},
		{api.OverallStatePartiallyFailed, true},
		{api.OverallStateFailed, false},
		{api.OverallStateCancelled, false},
		{api.OverallStatePending, false},
		{api.OverallStateRunning, false},
		{"", false},
	} {
		t.Run(string(tc.state), func(t *testing.T) {
			job := testEvaluationJob()
			job.Status.State = tc.state
			exported := make(chan struct{}, 1)
			h := &Handlers{resultsExporter: exportFunc(func(context.Context, *api.EvaluationJobResource, *cards.EvaluationCard) (evalcards.ExportResult, error) {
				exported <- struct{}{}
				return evalcards.ExportResult{}, nil
			})}
			h.exportEvaluationResultsAsync(context.Background(), nil, job, nil)
			if tc.want {
				awaitExportSignal(t, exported)
			} else {
				select {
				case <-exported:
					t.Fatal("ineligible job was exported")
				default:
				}
			}
		})
	}
	t.Run("missing status", func(t *testing.T) {
		job := testEvaluationJob()
		job.Status = nil
		exporter := &stubResultsExporter{}
		h := &Handlers{resultsExporter: exporter}
		h.exportEvaluationResultsAsync(context.Background(), nil, job, nil)
		if exporter.card != nil {
			t.Fatal("card exported without job status")
		}
	})
}

func testEvaluationJob() *api.EvaluationJobResource {
	return &api.EvaluationJobResource{
		Resource: api.EvaluationResource{Resource: api.Resource{ID: "job-1"}},
		Status: &api.EvaluationJobStatus{
			EvaluationJobState: api.EvaluationJobState{State: api.OverallStateCompleted},
		},
	}
}

func TestExportEvaluationResultsNilExporter(t *testing.T) {
	t.Parallel()
	h := &Handlers{}
	h.exportEvaluationResults(context.Background(), nil, testEvaluationJob(), nil)
}

func TestExportEvaluationResultsExportsCard(t *testing.T) {
	t.Parallel()
	exporter := &stubResultsExporter{cardURL: "https://example.com/card.json"}
	h := &Handlers{resultsExporter: exporter}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	h.exportEvaluationResults(context.Background(), nil, testEvaluationJob(), logger)
}

func TestExportEvaluationResultsExportError(t *testing.T) {
	t.Parallel()
	h := &Handlers{resultsExporter: &stubResultsExporter{err: errors.New("mlflow unavailable")}}

	h.exportEvaluationResults(context.Background(), nil, testEvaluationJob(), nil)
}

func TestExportEvaluationResultsAsyncNilJob(t *testing.T) {
	t.Parallel()
	h := &Handlers{resultsExporter: &stubResultsExporter{cardURL: "https://example.com/card.json"}}

	h.exportEvaluationResultsAsync(context.Background(), nil, nil, nil)
}

// TestPostProcessingSkipsEvaluationExports protects both direct OCI inputs and
// registry configuration copied from an eval_job, without changing evaluations.
func TestPostProcessingSkipsEvaluationExports(t *testing.T) {
	for _, source := range []string{"oci", "eval_job"} {
		t.Run(source, func(t *testing.T) {
			ref := &api.PostProcessingResultsDataRef{OCI: &api.OCIDataRef{}}
			if source == "eval_job" {
				ref = &api.PostProcessingResultsDataRef{EvalJob: &api.EvaluationJobDataRef{ID: "source"}}
			}
			job := testEvaluationJob()
			job.EvaluationJobConfig = *postprocessing.ToEvaluationJob(&api.StandalonePostProcessingRequest{
				Operations: api.StandalonePostProcessingOperations{ConfidenceInterval: &api.StandaloneConfidenceIntervalConfig{ResultsDataRef: ref}},
			})
			// eval_job inputs inherit this target from the source evaluation.
			if job.Exports == nil {
				job.Exports = &api.EvaluationExports{OCI: &api.EvaluationExportsOCI{}}
			}
			calls := make(chan struct{}, 1)
			h := &Handlers{validate: newOCIWorkflowValidator(t), resultsExporter: exportFunc(func(context.Context, *api.EvaluationJobResource, *cards.EvaluationCard) (evalcards.ExportResult, error) {
				calls <- struct{}{}
				return evalcards.ExportResult{}, nil
			})}
			h.exportEvaluationResults(context.Background(), nil, job, nil)
			h.exportEvaluationResultsAsync(context.Background(), nil, job, nil)
			select {
			case <-calls:
				t.Fatal("post-processing input triggered an evaluation-card export")
			case <-time.After(50 * time.Millisecond):
			}
			if job.Results != nil {
				t.Fatal("post-processing input acquired OCI export status")
			}
			if job.Exports.OCI == nil {
				t.Fatal("sidecar input configuration was removed")
			}
		})
	}
}
