package handlers

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/evalcards"
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

func TestExportEvaluationResultsEligibleStates(t *testing.T) {
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
			exporter := &stubResultsExporter{}
			h := &Handlers{resultsExporter: exporter}
			h.exportEvaluationResults(context.Background(), job, nil)
			if got := exporter.card != nil; got != tc.want {
				t.Fatalf("card exported = %v, want %v", got, tc.want)
			}
		})
	}
	t.Run("missing status", func(t *testing.T) {
		job := testEvaluationJob()
		job.Status = nil
		exporter := &stubResultsExporter{}
		h := &Handlers{resultsExporter: exporter}
		h.exportEvaluationResults(context.Background(), job, nil)
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
	h.exportEvaluationResults(context.Background(), testEvaluationJob(), nil)
}

func TestExportEvaluationResultsExportsCard(t *testing.T) {
	t.Parallel()
	exporter := &stubResultsExporter{cardURL: "https://example.com/card.json"}
	h := &Handlers{resultsExporter: exporter}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	h.exportEvaluationResults(context.Background(), testEvaluationJob(), logger)
}

func TestExportEvaluationResultsExportError(t *testing.T) {
	t.Parallel()
	h := &Handlers{resultsExporter: &stubResultsExporter{err: errors.New("mlflow unavailable")}}

	h.exportEvaluationResults(context.Background(), testEvaluationJob(), nil)
}

func TestExportEvaluationResultsNilJob(t *testing.T) {
	t.Parallel()
	h := &Handlers{resultsExporter: &stubResultsExporter{cardURL: "https://example.com/card.json"}}

	h.exportEvaluationResults(context.Background(), nil, nil)
}
