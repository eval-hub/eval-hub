package handlers

import (
	"context"
	"testing"

	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/eval-hub/eval-hub/pkg/cards"
)

type terminalTestExporter struct {
	called bool
}

func (e *terminalTestExporter) Export(_ context.Context, _ *api.EvaluationJobResource, _ *cards.EvaluationCard) (string, error) {
	e.called = true
	return "https://example.com/card.json", nil
}

type terminalTestStorage struct {
	noopStorage
	job      *api.EvaluationJobResource
	updateID string
	cardURL  string
}

func (s *terminalTestStorage) GetEvaluationJob(_ string) (*api.EvaluationJobResource, error) {
	return s.job, nil
}

func (s *terminalTestStorage) UpdateEvaluationJobCardURL(id string, cardURL string) error {
	s.updateID = id
	s.cardURL = cardURL
	return nil
}

func TestOnEvaluationJobUpdatedSkipsExportWhenNotTerminal(t *testing.T) {
	t.Parallel()
	exporter := &terminalTestExporter{}
	storage := &terminalTestStorage{
		job: &api.EvaluationJobResource{
			Resource: api.EvaluationResource{Resource: api.Resource{ID: "job-1"}},
			Status: &api.EvaluationJobStatus{
				EvaluationJobState: api.EvaluationJobState{State: api.OverallStateRunning},
			},
		},
	}
	h := &Handlers{resultsExporter: exporter}

	h.onEvaluationJobUpdated(
		context.Background(),
		storage,
		func() (*api.EvaluationJobResource, error) { return storage.job, nil },
		api.OverallStatePending,
		nil,
	)

	if exporter.called {
		t.Fatal("expected export to be skipped for non-terminal job")
	}
}

func TestOnEvaluationJobUpdatedSkipsExportWhenTerminalStateUnchanged(t *testing.T) {
	t.Parallel()
	exporter := &terminalTestExporter{}
	storage := &terminalTestStorage{
		job: &api.EvaluationJobResource{
			Resource: api.EvaluationResource{Resource: api.Resource{ID: "job-1"}},
			Status: &api.EvaluationJobStatus{
				EvaluationJobState: api.EvaluationJobState{State: api.OverallStateCompleted},
			},
		},
	}
	h := &Handlers{resultsExporter: exporter}

	h.onEvaluationJobUpdated(
		context.Background(),
		storage,
		func() (*api.EvaluationJobResource, error) { return storage.job, nil },
		api.OverallStateCompleted,
		nil,
	)

	if exporter.called {
		t.Fatal("expected export to be skipped when terminal state did not change")
	}
}

func TestOnEvaluationJobUpdatedExportsOnFailedTransition(t *testing.T) {
	t.Parallel()
	exporter := &terminalTestExporter{}
	storage := &terminalTestStorage{
		job: &api.EvaluationJobResource{
			Resource: api.EvaluationResource{Resource: api.Resource{ID: "job-1"}},
			Status: &api.EvaluationJobStatus{
				EvaluationJobState: api.EvaluationJobState{State: api.OverallStateFailed},
			},
		},
	}
	h := &Handlers{resultsExporter: exporter}

	h.onEvaluationJobUpdated(
		context.Background(),
		storage,
		func() (*api.EvaluationJobResource, error) { return storage.job, nil },
		api.OverallStateRunning,
		nil,
	)

	if !exporter.called {
		t.Fatal("expected export when job transitions to failed")
	}
	if storage.updateID != "job-1" || storage.cardURL != "https://example.com/card.json" {
		t.Fatalf("expected card URL persisted, got id=%q url=%q", storage.updateID, storage.cardURL)
	}
}
