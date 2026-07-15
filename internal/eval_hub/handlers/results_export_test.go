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

type evalCardPersistStorage struct {
	noopStorage
	called bool
}

func (s *evalCardPersistStorage) UpdateEvaluationJobEvalCard(_ string, _ []byte) error {
	s.called = true
	return nil
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
	storage := &evalCardPersistStorage{}
	h := &Handlers{}
	h.exportEvaluationResults(context.Background(), storage, testEvaluationJob(), nil)
	if !storage.called {
		t.Fatal("expected eval card to be persisted even without external exporter")
	}
}

func TestExportEvaluationResultsExportsCard(t *testing.T) {
	t.Parallel()
	exporter := &stubResultsExporter{cardURL: "https://example.com/card.json"}
	storage := &evalCardPersistStorage{}
	h := &Handlers{resultsExporter: exporter}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	h.exportEvaluationResults(context.Background(), storage, testEvaluationJob(), logger)
	if !storage.called {
		t.Fatal("expected eval card to be persisted")
	}
}

func TestExportEvaluationResultsExportError(t *testing.T) {
	t.Parallel()
	storage := &evalCardPersistStorage{}
	h := &Handlers{resultsExporter: &stubResultsExporter{err: errors.New("mlflow unavailable")}}

	h.exportEvaluationResults(context.Background(), storage, testEvaluationJob(), nil)
	if !storage.called {
		t.Fatal("expected eval card to be persisted before external export")
	}
}

func TestExportEvaluationResultsNilJob(t *testing.T) {
	t.Parallel()
	storage := &evalCardPersistStorage{}
	h := &Handlers{resultsExporter: &stubResultsExporter{cardURL: "https://example.com/card.json"}}

	h.exportEvaluationResults(context.Background(), storage, nil, nil)
	if storage.called {
		t.Fatal("expected eval card persistence to be skipped for nil job")
	}
}
