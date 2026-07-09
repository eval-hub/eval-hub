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
	job           *api.EvaluationJobResource
	cardURL       string
	updateID      string
	updateCardErr error
	getJobErr     error
}

func (s *exportResultsStorage) GetEvaluationJob(_ string) (*api.EvaluationJobResource, error) {
	if s.getJobErr != nil {
		return nil, s.getJobErr
	}
	return s.job, nil
}

func (s *exportResultsStorage) UpdateEvaluationJobCardURL(id string, cardURL string) error {
	if s.updateCardErr != nil {
		return s.updateCardErr
	}
	s.updateID = id
	s.cardURL = cardURL
	return nil
}

func TestExportEvaluationResultsNilExporter(t *testing.T) {
	t.Parallel()
	h := &Handlers{}
	h.exportEvaluationResults(context.Background(), &exportResultsStorage{}, "job-1", nil)
}

func TestExportEvaluationResultsPersistsCardURL(t *testing.T) {
	t.Parallel()
	storage := &exportResultsStorage{
		job: &api.EvaluationJobResource{
			Resource: api.EvaluationResource{Resource: api.Resource{ID: "job-1"}},
			Status: &api.EvaluationJobStatus{
				EvaluationJobState: api.EvaluationJobState{State: api.OverallStateCompleted},
			},
		},
	}
	h := &Handlers{resultsExporter: &stubResultsExporter{cardURL: "https://example.com/card.json"}}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	h.exportEvaluationResults(context.Background(), storage, "job-1", logger)

	if storage.updateID != "job-1" || storage.cardURL != "https://example.com/card.json" {
		t.Fatalf("expected card URL persisted, got id=%q url=%q", storage.updateID, storage.cardURL)
	}
}

func TestExportEvaluationResultsSkipsWhenExportReturnsEmptyURL(t *testing.T) {
	t.Parallel()
	storage := &exportResultsStorage{
		job: &api.EvaluationJobResource{
			Resource: api.EvaluationResource{Resource: api.Resource{ID: "job-1"}},
		},
	}
	h := &Handlers{resultsExporter: &stubResultsExporter{}}

	h.exportEvaluationResults(context.Background(), storage, "job-1", nil)

	if storage.updateID != "" {
		t.Fatalf("expected no card URL persistence, got update for %q", storage.updateID)
	}
}

func TestExportEvaluationResultsSkipsPersistWhenExportErrors(t *testing.T) {
	t.Parallel()
	storage := &exportResultsStorage{
		job: &api.EvaluationJobResource{
			Resource: api.EvaluationResource{Resource: api.Resource{ID: "job-1"}},
		},
	}
	h := &Handlers{resultsExporter: &stubResultsExporter{err: errors.New("mlflow unavailable")}}

	h.exportEvaluationResults(context.Background(), storage, "job-1", nil)

	if storage.updateID != "" {
		t.Fatalf("expected no card URL persistence on export error, got update for %q", storage.updateID)
	}
}
