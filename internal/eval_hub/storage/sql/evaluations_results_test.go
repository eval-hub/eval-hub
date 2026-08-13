package sql

import (
	"log/slog"
	"testing"

	"github.com/eval-hub/eval-hub/pkg/api"
)

// testStorage returns a minimal sqlStorage sufficient for computeBenchmarkTestResult
// tests that do not require a database connection (primary score is already resolved
// on the benchmark config so the provider lookup branch is never taken).
func testResultsStorage() *sqlStorage {
	return &sqlStorage{logger: slog.Default()}
}

func threshold32(v float32) *float32 { return &v }

func jobWithBenchmark(id, providerID string, primaryScore *api.PrimaryScore, passCriteria *api.PassCriteria) *api.EvaluationJobResource {
	return &api.EvaluationJobResource{
		EvaluationJobConfig: api.EvaluationJobConfig{
			Benchmarks: []api.EvaluationBenchmarkConfig{
				{
					Ref:          api.Ref{ID: id},
					ProviderID:   providerID,
					PrimaryScore: primaryScore,
					PassCriteria: passCriteria,
				},
			},
		},
	}
}

func statusEvent(id, providerID string, metrics map[string]any) *api.BenchmarkStatusEvent {
	return &api.BenchmarkStatusEvent{
		ID:         id,
		ProviderID: providerID,
		Metrics:    metrics,
	}
}

func TestComputeBenchmarkTestResult_NoBenchmarks(t *testing.T) {
	t.Parallel()
	s := testResultsStorage()
	job := &api.EvaluationJobResource{}
	result := s.computeBenchmarkTestResult(nil, job, statusEvent("b1", "p1", nil), nil)
	if result != nil {
		t.Fatalf("expected nil for job with no benchmarks, got %+v", result)
	}
}

func TestComputeBenchmarkTestResult_BenchmarkIDMismatch(t *testing.T) {
	t.Parallel()
	s := testResultsStorage()
	job := jobWithBenchmark("b1", "p1",
		&api.PrimaryScore{Metric: "b1.em"},
		&api.PassCriteria{Threshold: threshold32(0.5)},
	)
	result := s.computeBenchmarkTestResult(nil, job, statusEvent("other", "p1", map[string]any{"b1.em": float64(0.8)}), nil)
	if result != nil {
		t.Fatalf("expected nil when benchmark ID does not match, got %+v", result)
	}
}

func TestComputeBenchmarkTestResult_MetricNotFound(t *testing.T) {
	t.Parallel()
	s := testResultsStorage()
	job := jobWithBenchmark("hellaswag", "lighteval",
		&api.PrimaryScore{Metric: "hellaswag.em"},
		&api.PassCriteria{Threshold: threshold32(0.99)},
	)
	// Status event has acc_norm but not em — the bug that triggered this fix.
	event := statusEvent("hellaswag", "lighteval", map[string]any{"hellaswag.acc_norm": float64(0.3)})
	result := s.computeBenchmarkTestResult(nil, job, event, nil)
	if result != nil {
		t.Fatalf("expected nil when primary metric is absent from status event, got %+v", result)
	}
}

func TestComputeBenchmarkTestResult_PassesThreshold(t *testing.T) {
	t.Parallel()
	s := testResultsStorage()
	job := jobWithBenchmark("hellaswag", "lighteval",
		&api.PrimaryScore{Metric: "hellaswag.em"},
		&api.PassCriteria{Threshold: threshold32(0.25)},
	)
	event := statusEvent("hellaswag", "lighteval", map[string]any{"hellaswag.em": float64(0.45)})
	result := s.computeBenchmarkTestResult(nil, job, event, nil)
	if result == nil {
		t.Fatal("expected non-nil BenchmarkTest")
	}
	if !result.Pass {
		t.Errorf("expected Pass=true for score 0.45 >= threshold 0.25")
	}
	if result.PrimaryScore != 0.45 {
		t.Errorf("PrimaryScore = %v, want 0.45", result.PrimaryScore)
	}
	if result.Threshold != 0.25 {
		t.Errorf("Threshold = %v, want 0.25", result.Threshold)
	}
	if result.PrimaryScoreMetric != "hellaswag.em" {
		t.Errorf("PrimaryScoreMetric = %q, want %q", result.PrimaryScoreMetric, "hellaswag.em")
	}
}

func TestComputeBenchmarkTestResult_FailsThreshold(t *testing.T) {
	t.Parallel()
	s := testResultsStorage()
	job := jobWithBenchmark("hellaswag", "lighteval",
		&api.PrimaryScore{Metric: "hellaswag.em"},
		&api.PassCriteria{Threshold: threshold32(0.99)},
	)
	event := statusEvent("hellaswag", "lighteval", map[string]any{"hellaswag.em": float64(0.1)})
	result := s.computeBenchmarkTestResult(nil, job, event, nil)
	if result == nil {
		t.Fatal("expected non-nil BenchmarkTest")
	}
	if result.Pass {
		t.Errorf("expected Pass=false for score 0.1 < threshold 0.99")
	}
}

func TestComputeBenchmarkTestResult_LowerIsBetter(t *testing.T) {
	t.Parallel()
	s := testResultsStorage()
	job := jobWithBenchmark("perplexity", "lighteval",
		&api.PrimaryScore{Metric: "perplexity.score", LowerIsBetter: true},
		&api.PassCriteria{Threshold: threshold32(10.0)},
	)
	// score 8.0 <= threshold 10.0 → pass
	event := statusEvent("perplexity", "lighteval", map[string]any{"perplexity.score": float64(8.0)})
	result := s.computeBenchmarkTestResult(nil, job, event, nil)
	if result == nil {
		t.Fatal("expected non-nil BenchmarkTest")
	}
	if !result.Pass {
		t.Errorf("expected Pass=true for lower-is-better score 8.0 <= threshold 10.0")
	}

	// score 12.0 > threshold 10.0 → fail
	event2 := statusEvent("perplexity", "lighteval", map[string]any{"perplexity.score": float64(12.0)})
	result2 := s.computeBenchmarkTestResult(nil, job, event2, nil)
	if result2 == nil {
		t.Fatal("expected non-nil BenchmarkTest")
	}
	if result2.Pass {
		t.Errorf("expected Pass=false for lower-is-better score 12.0 > threshold 10.0")
	}
}

func TestComputeBenchmarkTestResult_NilPassCriteriaReturnsNil(t *testing.T) {
	t.Parallel()
	s := testResultsStorage()
	// No pass criteria on benchmark or provider (no db) → should return nil
	job := jobWithBenchmark("hellaswag", "lighteval",
		&api.PrimaryScore{Metric: "hellaswag.em"},
		nil, // no pass criteria
	)
	event := statusEvent("hellaswag", "lighteval", map[string]any{"hellaswag.em": float64(0.4)})
	result := s.computeBenchmarkTestResult(nil, job, event, nil)
	if result != nil {
		t.Fatalf("expected nil when no pass criteria is configured, got %+v", result)
	}
}

func TestComputeBenchmarkTestResult_Float32MetricValue(t *testing.T) {
	t.Parallel()
	s := testResultsStorage()
	job := jobWithBenchmark("hellaswag", "lighteval",
		&api.PrimaryScore{Metric: "hellaswag.em"},
		&api.PassCriteria{Threshold: threshold32(0.5)},
	)
	// Metric value as float32 (not float64)
	event := statusEvent("hellaswag", "lighteval", map[string]any{"hellaswag.em": float32(0.7)})
	result := s.computeBenchmarkTestResult(nil, job, event, nil)
	if result == nil {
		t.Fatal("expected non-nil BenchmarkTest for float32 metric value")
	}
	if !result.Pass {
		t.Errorf("expected Pass=true for score 0.7 >= threshold 0.5")
	}
}

func TestComputeBenchmarkTestResult_BenchmarkPassCriteriaTakesPrecedence(t *testing.T) {
	t.Parallel()
	// Regression: benchmark-level pass_criteria must override provider default.
	// Before the SDK fix, pass_criteria was silently dropped by Pydantic, so
	// the server fell back to the provider default (0.25) and never fired a violation.
	s := testResultsStorage()
	job := jobWithBenchmark("hellaswag", "lighteval",
		&api.PrimaryScore{Metric: "hellaswag.em"},
		&api.PassCriteria{Threshold: threshold32(0.99)}, // high threshold → should fail
	)
	// score 0.3 is above the provider default (0.25) but below the benchmark override (0.99)
	event := statusEvent("hellaswag", "lighteval", map[string]any{"hellaswag.em": float64(0.3)})
	result := s.computeBenchmarkTestResult(nil, job, event, nil)
	if result == nil {
		t.Fatal("expected non-nil BenchmarkTest")
	}
	if result.Pass {
		t.Errorf("expected Pass=false: score 0.3 is below the benchmark-level threshold 0.99")
	}
	if result.Threshold != 0.99 {
		t.Errorf("Threshold = %v, want 0.99 (benchmark override)", result.Threshold)
	}
}

func TestComputeBenchmarkTestResult_AccMetric(t *testing.T) {
	t.Parallel()
	s := testResultsStorage()
	job := jobWithBenchmark("toxigen", "lm_evaluation_harness",
		&api.PrimaryScore{Metric: "acc", LowerIsBetter: false},
		&api.PassCriteria{Threshold: threshold32(0.85)},
	)
	event := statusEvent("toxigen", "lm_evaluation_harness", map[string]any{
		"acc":      float64(0.6914983164983165),
		"acc_norm": float64(0.6372053872053872),
	})
	result := s.computeBenchmarkTestResult(nil, job, event, nil)
	if result == nil {
		t.Fatal("expected non-nil BenchmarkTest for toxigen acc metric")
	}
	if result.PrimaryScoreMetric != "acc" {
		t.Errorf("PrimaryScoreMetric = %q, want acc", result.PrimaryScoreMetric)
	}
	if result.Pass {
		t.Errorf("expected Pass=false for acc 0.69 below threshold 0.85")
	}
}

func TestComputeBenchmarkTestResult_MissingPrimaryMetricReturnsNil(t *testing.T) {
	t.Parallel()
	s := testResultsStorage()
	job := jobWithBenchmark("toxigen", "lm_evaluation_harness",
		&api.PrimaryScore{Metric: "toxicity_score", LowerIsBetter: true},
		&api.PassCriteria{Threshold: threshold32(0.3)},
	)
	event := statusEvent("toxigen", "lm_evaluation_harness", map[string]any{
		"acc":      float64(0.69),
		"acc_norm": float64(0.64),
	})
	result := s.computeBenchmarkTestResult(nil, job, event, nil)
	if result != nil {
		t.Fatalf("expected nil when primary metric is missing from metrics, got %+v", result)
	}
}
