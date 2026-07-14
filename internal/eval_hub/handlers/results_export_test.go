package handlers

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/eval-hub/eval-hub/pkg/cards"
)

type stubResultsExporter struct {
	cardURL string
	err     error
}

func (s *stubResultsExporter) Export(_ context.Context, _ *api.EvaluationJobResource, _ *cards.EvaluationCard) (string, error) {
	return s.cardURL, s.err
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
