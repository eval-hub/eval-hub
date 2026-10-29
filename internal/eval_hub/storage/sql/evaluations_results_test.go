package sql

import (
	"encoding/json"
	"log/slog"
	"math"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
	"github.com/eval-hub/eval-hub/internal/testhelpers"
	"github.com/eval-hub/eval-hub/pkg/api"
)

// testStorage returns a minimal sqlStorage sufficient for computeBenchmarkTestResult
// tests that do not require a database connection (primary score is already resolved
// on the benchmark config so the provider lookup branch is never taken).
func testResultsStorage() *sqlStorage {
	return &sqlStorage{logger: slog.Default()}
}

func threshold32(v float32) *float32 { return &v }

func TestCollectionLiveCodeBench_AdapterMetric(t *testing.T) {
	s := testResultsStorage()
	collections, err := config.LoadCollectionConfigs(s.logger, testhelpers.NewValidator(t), "../../../../config")
	if err != nil {
		t.Fatal(err)
	}
	const metric = "lcb:codegeneration_v6.codegen_pass@1"
	for _, id := range []string{"coding-v1", "software-v1", "standard-llm-evals-v1"} {
		t.Run(id, func(t *testing.T) {
			collection, ok := collections[id]
			if !ok {
				t.Fatalf("collection %s not found", id)
			}
			job := &api.EvaluationJobResource{
				EvaluationJobConfig: api.EvaluationJobConfig{Collection: &api.CollectionRef{ID: id}},
				Results:             &api.EvaluationJobResults{},
			}
			var weightedSum, totalWeight float32
			found := false
			for i, row := range collection.Benchmarks {
				score := float32(0.8)
				metricName := row.PrimaryScore.Metric
				if row.ID == "lcb:codegeneration_v6" && row.ProviderID == "lighteval" {
					found = true
					// Use the emitted adapter key independently of the configured selector.
					metricName, score = metric, 0.4
				}
				result := s.computeBenchmarkTestResult(nil, job, statusEvent(row.ID, row.ProviderID,
					map[string]any{metricName: score}), &collection)
				if result == nil || result.PrimaryScoreMetric != metricName || result.PrimaryScore != score || !result.Pass {
					t.Fatalf("unexpected result for %s: %+v", row.ID, result)
				}
				job.Results.Benchmarks = append(job.Results.Benchmarks, api.BenchmarkResult{
					ID: row.ID, ProviderID: row.ProviderID, BenchmarkIndex: i, Test: result,
				})
				weightedSum += row.Weight * score
				totalWeight += row.Weight
			}
			if !found {
				t.Fatal("LiveCodeBench benchmark not found")
			}
			s.computeJobTestResult(job, &collection)
			want := weightedSum / totalWeight
			if job.Results.Test == nil || math.Abs(float64(job.Results.Test.Score-want)) > 1e-6 || !job.Results.Test.Pass {
				t.Fatalf("aggregate = %+v, want score %v including LiveCodeBench", job.Results.Test, want)
			}
		})
	}
}

// Use the shipped collection definitions and the real row/aggregate scoring paths.
// In particular, rounding the Safety/Fairness aggregate target upward must not
// reject a result that meets every row target.
func TestCollectionThresholds_AtAndAroundRowTargets(t *testing.T) {
	s := testResultsStorage()
	collections, err := config.LoadCollectionConfigs(s.logger, testhelpers.NewValidator(t), "../../../../config")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"model-validation", "safety-and-fairness-v1", "reasoning-v1", "instruction-following-v1"} {
		t.Run(id, func(t *testing.T) {
			collection, ok := collections[id]
			if !ok {
				t.Fatalf("collection %s not found", id)
			}
			for _, tc := range []struct {
				name        string
				improvement float32
				wantPass    bool
			}{
				{name: "at_targets", wantPass: true},
				{name: "better_than_targets", improvement: 0.01, wantPass: true},
				{name: "worse_than_targets", improvement: -0.01, wantPass: false},
			} {
				t.Run(tc.name, func(t *testing.T) {
					job := &api.EvaluationJobResource{
						EvaluationJobConfig: api.EvaluationJobConfig{
							Collection: &api.CollectionRef{ID: id},
						},
						Results: &api.EvaluationJobResults{},
					}
					for i, row := range collection.Benchmarks {
						score := *row.PassCriteria.Threshold + tc.improvement
						if row.PrimaryScore.LowerIsBetter {
							score = *row.PassCriteria.Threshold - tc.improvement
						}
						event := statusEvent(row.ID, row.ProviderID, map[string]any{row.PrimaryScore.Metric: score})
						rowResult := s.computeBenchmarkTestResult(nil, job, event, &collection)
						if rowResult == nil || rowResult.Pass != tc.wantPass {
							t.Fatalf("unexpected row result for %s: %+v", row.ID, rowResult)
						}
						job.Results.Benchmarks = append(job.Results.Benchmarks, api.BenchmarkResult{
							ID: row.ID, ProviderID: row.ProviderID, BenchmarkIndex: i, Test: rowResult,
						})
					}
					s.computeJobTestResult(job, &collection)
					if job.Results.Test == nil || job.Results.Test.Pass != tc.wantPass {
						t.Fatalf("unexpected aggregate result: %+v; want pass=%v", job.Results.Test, tc.wantPass)
					}
				})
			}
		})
	}
}

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

func TestComputeBenchmarkTestResult_EventPrimaryScoreFallback(t *testing.T) {
	t.Parallel()
	s := testResultsStorage()
	// Job/catalog primary does not match emitted metrics (inspect/custom case).
	job := jobWithBenchmark("inspect/custom", "inspect",
		&api.PrimaryScore{Metric: "accuracy/accuracy"},
		&api.PassCriteria{Threshold: threshold32(0.15)},
	)
	event := statusEvent("inspect/custom", "inspect", map[string]any{
		"choice/accuracy": float64(0.2),
		"choice/stderr":   float64(0.01),
	})
	event.PrimaryScore = &api.PrimaryScore{Metric: "choice/accuracy"}
	result := s.computeBenchmarkTestResult(nil, job, event, nil)
	if result == nil {
		t.Fatal("expected test result from event primary_score fallback")
	}
	if result.PrimaryScoreMetric != "choice/accuracy" {
		t.Errorf("PrimaryScoreMetric = %q, want choice/accuracy", result.PrimaryScoreMetric)
	}
	if result.PrimaryScore != 0.2 {
		t.Errorf("PrimaryScore = %v, want 0.2", result.PrimaryScore)
	}
	if !result.Pass {
		t.Errorf("expected Pass=true for 0.2 >= 0.15")
	}
}

// Event-only lower_is_better must drive both benchmark Pass and job aggregate
// inversion when the job/catalog has no matching PrimaryScore.
func TestComputeJobTestResult_EventOnlyLowerIsBetter(t *testing.T) {
	t.Parallel()
	s := testResultsStorage()
	// No job PrimaryScore — direction comes only from the completed event.
	job := jobWithBenchmark("inspect/custom", "inspect", nil, &api.PassCriteria{Threshold: threshold32(0.3)})
	job.PassCriteria = &api.PassCriteria{Threshold: threshold32(0.5)}
	event := statusEvent("inspect/custom", "inspect", map[string]any{
		"attack_success_rate": float64(0.1),
	})
	event.PrimaryScore = &api.PrimaryScore{Metric: "attack_success_rate", LowerIsBetter: true}

	result := s.computeBenchmarkTestResult(nil, job, event, nil)
	if result == nil {
		t.Fatal("expected benchmark test from event primary_score")
	}
	if result.LowerIsBetter == nil || !*result.LowerIsBetter {
		t.Fatal("expected LowerIsBetter=true persisted on BenchmarkTest")
	}
	// 0.1 <= 0.3 → pass at benchmark level
	if !result.Pass {
		t.Fatalf("benchmark Pass=false, want true for lower-is-better 0.1 <= 0.3; got %+v", result)
	}

	job.Results = &api.EvaluationJobResults{
		Benchmarks: []api.BenchmarkResult{{
			ID: "inspect/custom", ProviderID: "inspect", BenchmarkIndex: 0, Test: result,
		}},
	}
	s.computeJobTestResult(job, nil)
	if job.Results.Test == nil {
		t.Fatal("expected job test aggregate")
	}
	// Aggregate must invert: score = 1 - 0.1 = 0.9 (>= job threshold 0.5 → pass)
	wantScore := float32(0.9)
	if math.Abs(float64(job.Results.Test.Score-wantScore)) > 1e-6 {
		t.Errorf("job Score = %v, want %v (inverted)", job.Results.Test.Score, wantScore)
	}
	if !job.Results.Test.Pass {
		t.Errorf("job Pass=false, want true so benchmark and job Pass agree")
	}
}

func TestComputeBenchmarkTestResult_JobPrimaryTakesPrecedenceOverEvent(t *testing.T) {
	t.Parallel()
	s := testResultsStorage()
	job := jobWithBenchmark("inspect/custom", "inspect",
		&api.PrimaryScore{Metric: "choice/accuracy"},
		&api.PassCriteria{Threshold: threshold32(0.5)},
	)
	event := statusEvent("inspect/custom", "inspect", map[string]any{
		"choice/accuracy": float64(0.2),
		"match/accuracy":  float64(0.9),
	})
	event.PrimaryScore = &api.PrimaryScore{Metric: "match/accuracy"}
	result := s.computeBenchmarkTestResult(nil, job, event, nil)
	if result == nil {
		t.Fatal("expected test result")
	}
	if result.PrimaryScoreMetric != "choice/accuracy" {
		t.Errorf("PrimaryScoreMetric = %q, want job choice/accuracy", result.PrimaryScoreMetric)
	}
}

// JSON round-trip must keep lower_is_better so computeJobTestResult still
// inverts after reloading results from storage (multi-benchmark jobs).
func TestComputeJobTestResult_LowerIsBetterSurvivesJSONRoundTrip(t *testing.T) {
	t.Parallel()
	s := testResultsStorage()
	job := jobWithBenchmark("inspect/custom", "inspect", nil, &api.PassCriteria{Threshold: threshold32(0.3)})
	job.PassCriteria = &api.PassCriteria{Threshold: threshold32(0.5)}
	event := statusEvent("inspect/custom", "inspect", map[string]any{
		"attack_success_rate": float64(0.1),
	})
	event.PrimaryScore = &api.PrimaryScore{Metric: "attack_success_rate", LowerIsBetter: true}

	result := s.computeBenchmarkTestResult(nil, job, event, nil)
	if result == nil {
		t.Fatal("expected benchmark test")
	}

	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var reloaded api.BenchmarkTest
	if err := json.Unmarshal(raw, &reloaded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if reloaded.LowerIsBetter == nil || !*reloaded.LowerIsBetter {
		t.Fatalf("lower_is_better lost after JSON round-trip; payload=%s", raw)
	}

	job.Results = &api.EvaluationJobResults{
		Benchmarks: []api.BenchmarkResult{{
			ID: "inspect/custom", ProviderID: "inspect", BenchmarkIndex: 0, Test: &reloaded,
		}},
	}
	s.computeJobTestResult(job, nil)
	if job.Results.Test == nil {
		t.Fatal("expected job test aggregate")
	}
	wantScore := float32(0.9)
	if math.Abs(float64(job.Results.Test.Score-wantScore)) > 1e-6 {
		t.Errorf("job Score = %v, want %v after reload", job.Results.Test.Score, wantScore)
	}
}

func TestSelectPrimaryScore_FirstNumericMatch(t *testing.T) {
	t.Parallel()
	job := &api.PrimaryScore{Metric: "job_metric"}
	event := &api.PrimaryScore{Metric: "event_metric"}
	got, value, ok := selectPrimaryScore(
		[]*api.PrimaryScore{job, event},
		map[string]any{"event_metric": float64(0.4)},
	)
	if !ok || got != event {
		t.Fatalf("expected event candidate, ok=%v got=%v", ok, got)
	}
	if value != 0.4 {
		t.Errorf("value = %v, want 0.4", value)
	}
}

func TestSelectPrimaryScore_SkipsNonNumericAndContinues(t *testing.T) {
	t.Parallel()
	job := &api.PrimaryScore{Metric: "accuracy/accuracy"}
	event := &api.PrimaryScore{Metric: "choice/accuracy"}
	got, value, ok := selectPrimaryScore(
		[]*api.PrimaryScore{job, event},
		map[string]any{
			"accuracy/accuracy": "n/a",
			"choice/accuracy":   float64(0.2),
		},
	)
	if !ok || got != event {
		t.Fatalf("expected event after non-numeric job metric, ok=%v got=%v", ok, got)
	}
	if value != 0.2 {
		t.Errorf("value = %v, want 0.2", value)
	}
}

func TestSelectPrimaryScore_NoMatch(t *testing.T) {
	t.Parallel()
	_, _, ok := selectPrimaryScore(
		[]*api.PrimaryScore{{Metric: "missing"}},
		map[string]any{"other": float64(1)},
	)
	if ok {
		t.Fatal("expected no match")
	}
}

func TestComputeBenchmarkTestResult_SkipsNonNumericJobPrimary(t *testing.T) {
	t.Parallel()
	s := testResultsStorage()
	job := jobWithBenchmark("inspect/custom", "inspect",
		&api.PrimaryScore{Metric: "accuracy/accuracy"},
		&api.PassCriteria{Threshold: threshold32(0.15)},
	)
	event := statusEvent("inspect/custom", "inspect", map[string]any{
		"accuracy/accuracy": "n/a",
		"choice/accuracy":   float64(0.2),
	})
	event.PrimaryScore = &api.PrimaryScore{Metric: "choice/accuracy"}
	result := s.computeBenchmarkTestResult(nil, job, event, nil)
	if result == nil {
		t.Fatal("expected fallback to numeric event primary_score")
	}
	if result.PrimaryScoreMetric != "choice/accuracy" {
		t.Errorf("PrimaryScoreMetric = %q, want choice/accuracy", result.PrimaryScoreMetric)
	}
	if result.PrimaryScore != 0.2 {
		t.Errorf("PrimaryScore = %v, want 0.2", result.PrimaryScore)
	}
}
