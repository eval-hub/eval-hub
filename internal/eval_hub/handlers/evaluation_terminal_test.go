package handlers

import (
	"context"
	"errors"
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
	job *api.EvaluationJobResource
}

type terminalEvalCardStorage struct {
	terminalTestStorage
	persistCalled bool
}

func (s *terminalEvalCardStorage) UpdateEvaluationJobEvalCard(_ string, _ []byte) error {
	s.persistCalled = true
	return nil
}

func (s *terminalTestStorage) GetEvaluationJob(_ string) (*api.EvaluationJobResource, error) {
	return s.job, nil
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
	storage := &terminalEvalCardStorage{
		terminalTestStorage: terminalTestStorage{
			job: &api.EvaluationJobResource{
				Resource: api.EvaluationResource{Resource: api.Resource{ID: "job-1"}},
				Status: &api.EvaluationJobStatus{
					EvaluationJobState: api.EvaluationJobState{State: api.OverallStateFailed},
				},
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
	if !storage.persistCalled {
		t.Fatal("expected eval card to be persisted when job transitions to failed")
	}
}

func TestOnEvaluationJobUpdatedSkipsWhenGetJobFails(t *testing.T) {
	t.Parallel()
	exporter := &terminalTestExporter{}
	h := &Handlers{resultsExporter: exporter}

	h.onEvaluationJobUpdated(
		context.Background(),
		&terminalTestStorage{},
		func() (*api.EvaluationJobResource, error) { return nil, errors.New("load failed") },
		api.OverallStateRunning,
		nil,
	)

	if exporter.called {
		t.Fatal("expected export to be skipped when getJob fails")
	}
}

func TestOnEvaluationJobUpdatedSkipsWhenJobNil(t *testing.T) {
	t.Parallel()
	exporter := &terminalTestExporter{}
	h := &Handlers{resultsExporter: exporter}

	h.onEvaluationJobUpdated(
		context.Background(),
		&terminalTestStorage{},
		func() (*api.EvaluationJobResource, error) { return nil, nil },
		api.OverallStateRunning,
		nil,
	)

	if exporter.called {
		t.Fatal("expected export to be skipped when job is nil")
	}
}

func TestResolveJobBenchmarksForStorageWithCollection(t *testing.T) {
	t.Parallel()
	storage := &collectionTerminalStorage{
		terminalTestStorage: terminalTestStorage{
			job: &api.EvaluationJobResource{
				EvaluationJobConfig: api.EvaluationJobConfig{
					Collection: &api.CollectionRef{ID: "col-1"},
				},
			},
		},
		collection: &api.CollectionResource{
			CollectionConfig: api.CollectionConfig{
				Benchmarks: []api.CollectionBenchmarkConfig{
					{Ref: api.Ref{ID: "arc_easy"}, ProviderID: "lm_evaluation_harness"},
				},
			},
		},
	}
	h := &Handlers{}

	benchmarks, err := h.resolveJobBenchmarksForStorage(storage, storage.job)
	if err != nil {
		t.Fatalf("resolveJobBenchmarksForStorage() err = %v", err)
	}
	if len(benchmarks) != 1 || benchmarks[0].ID != "arc_easy" {
		t.Fatalf("benchmarks = %#v", benchmarks)
	}
}

type collectionTerminalStorage struct {
	terminalTestStorage
	collection *api.CollectionResource
}

func (s *collectionTerminalStorage) GetCollection(_ string) (*api.CollectionResource, error) {
	return s.collection, nil
}
