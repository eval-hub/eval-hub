package watchdog

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/abstractions"
	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
	"github.com/eval-hub/eval-hub/internal/eval_hub/constants"
	"github.com/eval-hub/eval-hub/pkg/api"
)

type capturedUpdate struct {
	ID     string
	Status *api.StatusEvent
}

// sharedState holds mutable state shared across all copies of mockStorage
// created by With* methods.
type sharedState struct {
	mu      sync.Mutex
	updates []capturedUpdate
}

func (s *sharedState) addUpdate(u capturedUpdate) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.updates = append(s.updates, u)
}

func (s *sharedState) getUpdates() []capturedUpdate {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]capturedUpdate, len(s.updates))
	copy(cp, s.updates)
	return cp
}

// mockStorage implements the subset of abstractions.Storage used by the watchdog.
type mockStorage struct {
	abstractions.Storage
	jobs      []api.EvaluationJobResource
	updateErr error
	shared    *sharedState
}

func newMockStorage(jobs []api.EvaluationJobResource) *mockStorage {
	return &mockStorage{
		jobs:   jobs,
		shared: &sharedState{},
	}
}

func (m *mockStorage) GetEvaluationJobs(filter *abstractions.QueryFilter) (*abstractions.QueryResults[api.EvaluationJobResource], error) {
	return &abstractions.QueryResults[api.EvaluationJobResource]{
		Items:      m.jobs,
		TotalCount: len(m.jobs),
	}, nil
}

func (m *mockStorage) UpdateEvaluationJob(id string, runStatus *api.StatusEvent) error {
	m.shared.addUpdate(capturedUpdate{ID: id, Status: runStatus})
	return m.updateErr
}

func (m *mockStorage) WithContext(ctx context.Context) abstractions.Storage {
	return &mockStorage{Storage: m.Storage, jobs: m.jobs, updateErr: m.updateErr, shared: m.shared}
}

func (m *mockStorage) WithTenant(tenant api.Tenant) abstractions.Storage {
	return &mockStorage{Storage: m.Storage, jobs: m.jobs, updateErr: m.updateErr, shared: m.shared}
}

func (m *mockStorage) WithOwner(owner api.User) abstractions.Storage {
	return &mockStorage{Storage: m.Storage, jobs: m.jobs, updateErr: m.updateErr, shared: m.shared}
}

func (m *mockStorage) WithLogger(logger *slog.Logger) abstractions.Storage {
	return m
}

func (m *mockStorage) getUpdates() []capturedUpdate {
	return m.shared.getUpdates()
}

func newTestLogger() *slog.Logger {
	return slog.Default()
}

func TestWatchdog_FailsStuckBenchmark(t *testing.T) {
	t.Parallel()

	staleTime := time.Now().Add(-15 * time.Minute)

	store := newMockStorage([]api.EvaluationJobResource{
		{
			Resource: api.EvaluationResource{
				Resource: api.Resource{
					ID:        "job-1",
					UpdatedAt: staleTime,
					Tenant:    "test-tenant",
					Owner:     "test-user",
				},
			},
			Status: &api.EvaluationJobStatus{
				EvaluationJobState: api.EvaluationJobState{State: api.OverallStateRunning},
				Benchmarks: []api.BenchmarkStatus{
					{
						ID:             "bench-a",
						ProviderID:     "prov-1",
						BenchmarkIndex: 0,
						Status:         api.StateRunning,
						Phase:          api.JobPhaseRunningEvaluation,
						UpdatedAt:      api.DateTimeToString(staleTime),
					},
				},
			},
		},
	})

	svcConfig := &config.ServiceConfig{
		BenchmarkTimeout:          10 * time.Minute,
		BenchmarkWatchdogInterval: 50 * time.Millisecond,
	}

	wd := New(newTestLogger(), store, svcConfig)
	wd.scan(context.Background())

	updates := store.getUpdates()
	if len(updates) != 1 {
		t.Fatalf("expected 1 update, got %d", len(updates))
	}
	if updates[0].ID != "job-1" {
		t.Errorf("expected update for job-1, got %s", updates[0].ID)
	}
	event := updates[0].Status.BenchmarkStatusEvent
	if event.Status != api.StateFailed {
		t.Errorf("expected status failed, got %s", event.Status)
	}
	if event.ErrorMessage == nil || event.ErrorMessage.MessageCode != constants.MESSAGE_CODE_BENCHMARK_TIMEOUT {
		t.Error("expected benchmark_timeout message code")
	}
}

func TestWatchdog_SkipsTerminalBenchmarks(t *testing.T) {
	t.Parallel()

	staleTime := time.Now().Add(-15 * time.Minute)

	store := newMockStorage([]api.EvaluationJobResource{
		{
			Resource: api.EvaluationResource{
				Resource: api.Resource{
					ID:        "job-2",
					UpdatedAt: staleTime,
				},
			},
			Status: &api.EvaluationJobStatus{
				EvaluationJobState: api.EvaluationJobState{State: api.OverallStateRunning},
				Benchmarks: []api.BenchmarkStatus{
					{
						ID:             "bench-done",
						ProviderID:     "prov-1",
						BenchmarkIndex: 0,
						Status:         api.StateCompleted,
						UpdatedAt:      api.DateTimeToString(staleTime),
					},
					{
						ID:             "bench-failed",
						ProviderID:     "prov-1",
						BenchmarkIndex: 1,
						Status:         api.StateFailed,
						UpdatedAt:      api.DateTimeToString(staleTime),
					},
				},
			},
		},
	})

	svcConfig := &config.ServiceConfig{
		BenchmarkTimeout:          10 * time.Minute,
		BenchmarkWatchdogInterval: 50 * time.Millisecond,
	}

	wd := New(newTestLogger(), store, svcConfig)
	wd.scan(context.Background())

	updates := store.getUpdates()
	if len(updates) != 0 {
		t.Fatalf("expected 0 updates for terminal benchmarks, got %d", len(updates))
	}
}

func TestWatchdog_SkipsRecentBenchmarks(t *testing.T) {
	t.Parallel()

	recentTime := time.Now().Add(-2 * time.Minute)

	store := newMockStorage([]api.EvaluationJobResource{
		{
			Resource: api.EvaluationResource{
				Resource: api.Resource{
					ID:        "job-3",
					UpdatedAt: recentTime,
				},
			},
			Status: &api.EvaluationJobStatus{
				EvaluationJobState: api.EvaluationJobState{State: api.OverallStateRunning},
				Benchmarks: []api.BenchmarkStatus{
					{
						ID:             "bench-active",
						ProviderID:     "prov-1",
						BenchmarkIndex: 0,
						Status:         api.StateRunning,
						Phase:          api.JobPhaseRunningEvaluation,
						UpdatedAt:      api.DateTimeToString(recentTime),
					},
				},
			},
		},
	})

	svcConfig := &config.ServiceConfig{
		BenchmarkTimeout:          10 * time.Minute,
		BenchmarkWatchdogInterval: 50 * time.Millisecond,
	}

	wd := New(newTestLogger(), store, svcConfig)
	wd.scan(context.Background())

	updates := store.getUpdates()
	if len(updates) != 0 {
		t.Fatalf("expected 0 updates for recent benchmarks, got %d", len(updates))
	}
}

func TestWatchdog_FallsBackToStartedAt(t *testing.T) {
	t.Parallel()

	staleTime := time.Now().Add(-15 * time.Minute)

	store := newMockStorage([]api.EvaluationJobResource{
		{
			Resource: api.EvaluationResource{
				Resource: api.Resource{
					ID: "job-4",
				},
			},
			Status: &api.EvaluationJobStatus{
				EvaluationJobState: api.EvaluationJobState{State: api.OverallStateRunning},
				Benchmarks: []api.BenchmarkStatus{
					{
						ID:             "bench-old",
						ProviderID:     "prov-1",
						BenchmarkIndex: 0,
						Status:         api.StateRunning,
						StartedAt:      api.DateTimeToString(staleTime),
					},
				},
			},
		},
	})

	svcConfig := &config.ServiceConfig{
		BenchmarkTimeout:          10 * time.Minute,
		BenchmarkWatchdogInterval: 50 * time.Millisecond,
	}

	wd := New(newTestLogger(), store, svcConfig)
	wd.scan(context.Background())

	updates := store.getUpdates()
	if len(updates) != 1 {
		t.Fatalf("expected 1 update when falling back to StartedAt, got %d", len(updates))
	}
}

func TestWatchdog_MultipleBenchmarksMixedState(t *testing.T) {
	t.Parallel()

	staleTime := time.Now().Add(-15 * time.Minute)
	recentTime := time.Now().Add(-2 * time.Minute)

	store := newMockStorage([]api.EvaluationJobResource{
		{
			Resource: api.EvaluationResource{
				Resource: api.Resource{
					ID: "job-5",
				},
			},
			Status: &api.EvaluationJobStatus{
				EvaluationJobState: api.EvaluationJobState{State: api.OverallStateRunning},
				Benchmarks: []api.BenchmarkStatus{
					{
						ID:             "bench-stuck",
						ProviderID:     "prov-1",
						BenchmarkIndex: 0,
						Status:         api.StateRunning,
						UpdatedAt:      api.DateTimeToString(staleTime),
					},
					{
						ID:             "bench-ok",
						ProviderID:     "prov-1",
						BenchmarkIndex: 1,
						Status:         api.StateRunning,
						UpdatedAt:      api.DateTimeToString(recentTime),
					},
					{
						ID:             "bench-done",
						ProviderID:     "prov-1",
						BenchmarkIndex: 2,
						Status:         api.StateCompleted,
						UpdatedAt:      api.DateTimeToString(staleTime),
					},
				},
			},
		},
	})

	svcConfig := &config.ServiceConfig{
		BenchmarkTimeout:          10 * time.Minute,
		BenchmarkWatchdogInterval: 50 * time.Millisecond,
	}

	wd := New(newTestLogger(), store, svcConfig)
	wd.scan(context.Background())

	updates := store.getUpdates()
	if len(updates) != 1 {
		t.Fatalf("expected 1 update (only stuck benchmark), got %d", len(updates))
	}
	if updates[0].Status.BenchmarkStatusEvent.ID != "bench-stuck" {
		t.Errorf("expected bench-stuck to be failed, got %s", updates[0].Status.BenchmarkStatusEvent.ID)
	}
}

func TestWatchdog_StopsOnContextCancel(t *testing.T) {
	t.Parallel()

	store := newMockStorage(nil)
	svcConfig := &config.ServiceConfig{
		BenchmarkTimeout:          10 * time.Minute,
		BenchmarkWatchdogInterval: 50 * time.Millisecond,
	}

	wd := New(newTestLogger(), store, svcConfig)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		wd.Run(ctx)
		close(done)
	}()

	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// success
	case <-time.After(2 * time.Second):
		t.Fatal("watchdog did not stop after context cancellation")
	}
}

func TestServiceConfig_BenchmarkWatchdogEnabled(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		config   *config.ServiceConfig
		expected bool
	}{
		{"nil config", nil, false},
		{"zero timeout (default enabled)", &config.ServiceConfig{}, true},
		{"positive timeout", &config.ServiceConfig{BenchmarkTimeout: 5 * time.Minute}, true},
		{"negative timeout (disabled)", &config.ServiceConfig{BenchmarkTimeout: -1}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.config.BenchmarkWatchdogEnabled(); got != tt.expected {
				t.Errorf("BenchmarkWatchdogEnabled() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestServiceConfig_EffectiveBenchmarkTimeout(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		config   *config.ServiceConfig
		expected time.Duration
	}{
		{"nil config", nil, 10 * time.Minute},
		{"zero (default)", &config.ServiceConfig{}, 10 * time.Minute},
		{"custom", &config.ServiceConfig{BenchmarkTimeout: 5 * time.Minute}, 5 * time.Minute},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.config.EffectiveBenchmarkTimeout(); got != tt.expected {
				t.Errorf("EffectiveBenchmarkTimeout() = %v, want %v", got, tt.expected)
			}
		})
	}
}
