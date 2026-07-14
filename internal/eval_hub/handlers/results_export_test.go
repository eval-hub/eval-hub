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

type exportResultsStorage struct {
	noopStorage
	job       *api.EvaluationJobResource
	getJobErr error
}

func (s *exportResultsStorage) GetEvaluationJob(_ string) (*api.EvaluationJobResource, error) {
	if s.getJobErr != nil {
		return nil, s.getJobErr
	}
	return s.job, nil
}

func TestExportEvaluationResultsNilExporter(t *testing.T) {
	t.Parallel()
	h := &Handlers{}
	h.exportEvaluationResults(context.Background(), &exportResultsStorage{}, "job-1", nil)
}

func TestExportEvaluationResultsExportsCard(t *testing.T) {
	t.Parallel()
	storage := &exportResultsStorage{
		job: &api.EvaluationJobResource{
			Resource: api.EvaluationResource{Resource: api.Resource{ID: "job-1"}},
			Status: &api.EvaluationJobStatus{
				EvaluationJobState: api.EvaluationJobState{State: api.OverallStateCompleted},
			},
		},
	}
	exporter := &stubResultsExporter{cardURL: "https://example.com/card.json"}
	h := &Handlers{resultsExporter: exporter}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	h.exportEvaluationResults(context.Background(), storage, "job-1", logger)
}

func TestExportEvaluationResultsExportError(t *testing.T) {
	t.Parallel()
	storage := &exportResultsStorage{
		job: &api.EvaluationJobResource{
			Resource: api.EvaluationResource{Resource: api.Resource{ID: "job-1"}},
		},
	}
	h := &Handlers{resultsExporter: &stubResultsExporter{err: errors.New("mlflow unavailable")}}

	h.exportEvaluationResults(context.Background(), storage, "job-1", nil)
}

func TestExportEvaluationResultsGetJobError(t *testing.T) {
	t.Parallel()
	storage := &exportResultsStorage{getJobErr: errors.New("db unavailable")}
	h := &Handlers{resultsExporter: &stubResultsExporter{cardURL: "https://example.com/card.json"}}

	h.exportEvaluationResults(context.Background(), storage, "job-1", nil)
}

func TestExportEvaluationResultsNilJob(t *testing.T) {
	t.Parallel()
	storage := &exportResultsStorage{job: nil}
	h := &Handlers{resultsExporter: &stubResultsExporter{cardURL: "https://example.com/card.json"}}

	h.exportEvaluationResults(context.Background(), storage, "job-1", nil)
}
