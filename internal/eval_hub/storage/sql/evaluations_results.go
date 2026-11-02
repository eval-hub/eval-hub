package sql

import (
	"database/sql"
	"fmt"
	"reflect"
	"strings"

	"github.com/eval-hub/eval-hub/internal/eval_hub/handlers"
	"github.com/eval-hub/eval-hub/pkg/api"
)

func (s *sqlStorage) updateBenchmarkResults(job *api.EvaluationJobResource, runStatus *api.StatusEvent, result *api.BenchmarkResult) error {
	if job.Results == nil {
		job.Results = &api.EvaluationJobResults{}
	}
	if job.Results.Benchmarks == nil {
		job.Results.Benchmarks = make([]api.BenchmarkResult, 0)
	}

	for i, benchmark := range job.Results.Benchmarks {
		if benchmark.ID == runStatus.BenchmarkStatusEvent.ID &&
			benchmark.ProviderID == runStatus.BenchmarkStatusEvent.ProviderID &&
			benchmark.BenchmarkIndex == runStatus.BenchmarkStatusEvent.BenchmarkIndex {
			if reflect.DeepEqual(benchmark, *result) {
				return nil
			}
			job.Results.Benchmarks[i] = *result
			return nil
		}
	}

	job.Results.Benchmarks = append(job.Results.Benchmarks, *result)

	return nil
}

func (s *sqlStorage) computeJobTestResult(job *api.EvaluationJobResource, collection *api.CollectionResource) {
	if job.Results == nil || job.Results.Benchmarks == nil || len(job.Results.Benchmarks) == 0 {
		return
	}
	var sumOfWeightedScores float32 = 0.0
	var sumOfWeights float32 = 0.0
	resolvedJobBenchmarks, err := handlers.GetJobBenchmarks(job, collection)
	if err != nil {
		s.logger.Error("Failed to get job benchmarks", "error", err, "job_id", job.Resource.ID)
		return
	}
	for _, benchmark := range job.Results.Benchmarks {
		if benchmark.Test == nil {
			// if the benchmark test result is not defined, we skip it
			// This should never happen, since this method is called only when the overall job status is 'completed'
			s.logger.Info("Benchmark test result is not defined for benchmark", "benchmark_id", benchmark.ID, "benchmark_index", benchmark.BenchmarkIndex)
			continue
		}
		if benchmark.BenchmarkIndex < 0 || benchmark.BenchmarkIndex >= len(resolvedJobBenchmarks) {
			s.logger.Warn(
				"benchmark index out of range for resolved benchmarks",
				"benchmark_id", benchmark.ID,
				"benchmark_index", benchmark.BenchmarkIndex,
				"resolved_count", len(resolvedJobBenchmarks),
			)
			continue
		}
		benchmarkWeight := resolvedJobBenchmarks[benchmark.BenchmarkIndex].Weight
		if benchmarkWeight == 0 {
			// if the benchmark weight is not defined, we set it to 1
			benchmarkWeight = 1
		}
		// Prefer the direction stored on the benchmark test result (includes
		// event-reported LowerIsBetter persisted via JSON). Fall back to the
		// job/catalog PrimaryScore for legacy results that omit it.
		lowerIsBetter := false
		if benchmark.Test.LowerIsBetter != nil {
			lowerIsBetter = *benchmark.Test.LowerIsBetter
		} else if primaryScore := resolvedJobBenchmarks[benchmark.BenchmarkIndex].PrimaryScore; primaryScore != nil {
			lowerIsBetter = primaryScore.LowerIsBetter
		}
		weightedScore := benchmarkWeight * benchmark.Test.PrimaryScore
		if lowerIsBetter {
			weightedScore = benchmarkWeight * (1 - benchmark.Test.PrimaryScore)
		}
		sumOfWeightedScores += weightedScore
		sumOfWeights += benchmarkWeight
		s.logger.Info("Benchmark test result", "benchmark_id", benchmark.ID, "benchmark_index", benchmark.BenchmarkIndex, "primary_score", benchmark.Test.PrimaryScore, "weighted_score", weightedScore, "benchmark_weight", benchmarkWeight, "sum_of_weighted_scores", sumOfWeightedScores, "sum_of_weights", sumOfWeights)
	}
	if sumOfWeights == 0 {
		s.logger.Warn("No benchmark weights accumulated; cannot compute job score")
		return
	}
	weightedAvgJobScore := sumOfWeightedScores / sumOfWeights
	s.logger.Info("Weighted average job score", "weighted_avg_job_score", weightedAvgJobScore, "sum_of_weighted_scores", sumOfWeightedScores, "sum_of_weights", sumOfWeights)

	threshold := getPassCriteriaThreshold(job, collection)
	jobTest := &api.EvaluationTest{
		Score:     weightedAvgJobScore,
		Threshold: threshold,
		Pass:      weightedAvgJobScore >= threshold,
	}

	job.Results.Test = jobTest
}

func getPassCriteriaThreshold(job *api.EvaluationJobResource, collection *api.CollectionResource) float32 {
	if job.PassCriteria != nil && job.PassCriteria.Threshold != nil {
		return *job.PassCriteria.Threshold
	}
	if collection != nil && collection.PassCriteria != nil && collection.PassCriteria.Threshold != nil {
		return *collection.PassCriteria.Threshold
	}
	// this is the hard-coded default pass criteria threshold
	return 0.5
}

func (s *sqlStorage) computeBenchmarkTestResult(txn *sql.Tx, job *api.EvaluationJobResource, benchmarkStatusEvent *api.BenchmarkStatusEvent, collection *api.CollectionResource) *api.BenchmarkTest {
	// job could have benchmarks array or it could have collection. If it has collection, we need to get the benchmarks from the collection
	benchmarks, err := handlers.GetJobBenchmarks(job, collection)
	if err != nil {
		s.logger.Error("Failed to get job benchmarks", "error", err, "job_id", job.Resource.ID)
		return nil
	}
	if len(benchmarks) == 0 {
		return nil
	}
	for _, benchmark := range benchmarks {
		if benchmark.ID != benchmarkStatusEvent.ID || benchmark.ProviderID != benchmarkStatusEvent.ProviderID {
			continue
		}

		// Lazy provider load: only hit storage when job-level primary/pass_criteria
		// are insufficient (keeps unit tests that omit a DB working).
		var providerBench *api.BenchmarkResource
		providerLoaded := false
		loadProviderBench := func() {
			if providerLoaded || benchmark.ProviderID == "" {
				return
			}
			// Unit tests construct a logger-only sqlStorage without a statement factory.
			if s.statementsFactory == nil {
				providerLoaded = true
				return
			}
			providerLoaded = true
			provider, err := s.getUserProviderTransactional(txn, benchmark.ProviderID)
			if err == nil && provider != nil {
				for i := range provider.Benchmarks {
					if provider.Benchmarks[i].ID == benchmark.ID {
						providerBench = &provider.Benchmarks[i]
						return
					}
				}
			}
		}

		// Prefer the first primary_score whose metric is present in emitted metrics
		// and castable to float: job (explicit) → event (adapter-reported) →
		// provider catalog. Exact key match only — no service-side heuristics.
		jobPrimary := benchmark.PrimaryScore
		candidates := make([]*api.PrimaryScore, 0, 3)
		if jobPrimary != nil {
			candidates = append(candidates, jobPrimary)
		}
		if benchmarkStatusEvent.PrimaryScore != nil {
			candidates = append(candidates, benchmarkStatusEvent.PrimaryScore)
		}
		primaryScore, primaryMetricValueFloat, ok := selectPrimaryScore(candidates, benchmarkStatusEvent.Metrics)
		if !ok {
			loadProviderBench()
			if providerBench != nil && providerBench.PrimaryScore != nil {
				candidates = append(candidates, providerBench.PrimaryScore)
				primaryScore, primaryMetricValueFloat, ok = selectPrimaryScore(candidates, benchmarkStatusEvent.Metrics)
			}
		}
		if !ok {
			tried := primaryScoreMetricNames(candidates)
			if len(benchmarkStatusEvent.Metrics) > 0 && len(tried) > 0 {
				s.logger.Error("Primary score metric not present in benchmark metrics; test section omitted",
					"benchmark_id", benchmarkStatusEvent.ID,
					"provider_id", benchmarkStatusEvent.ProviderID,
					"tried_primary_metrics", strings.Join(tried, ","))
			}
			return nil
		}

		var threshold float32
		if benchmark.PassCriteria != nil && benchmark.PassCriteria.Threshold != nil {
			threshold = *benchmark.PassCriteria.Threshold
		} else {
			loadProviderBench()
			if providerBench != nil && providerBench.PassCriteria != nil && providerBench.PassCriteria.Threshold != nil {
				threshold = *providerBench.PassCriteria.Threshold
			} else {
				return nil
			}
		}

		// Visibility when an explicit job/collection primary was configured but
		// missing or non-numeric in metrics, so a lower-precedence candidate won.
		if jobPrimary != nil && jobPrimary.Metric != "" && primaryScore.Metric != jobPrimary.Metric {
			source := "event"
			if providerBench != nil && providerBench.PrimaryScore != nil && primaryScore == providerBench.PrimaryScore {
				source = "catalog"
			}
			s.logger.Warn("Configured primary_score.metric unavailable in metrics; using fallback",
				"benchmark_id", benchmarkStatusEvent.ID,
				"provider_id", benchmarkStatusEvent.ProviderID,
				"skipped_primary_metric", jobPrimary.Metric,
				"selected_primary_metric", primaryScore.Metric,
				"selected_source", source,
				"threshold", threshold,
				"lower_is_better", primaryScore.LowerIsBetter)
		}

		pass := primaryMetricValueFloat >= threshold
		if primaryScore.LowerIsBetter {
			pass = primaryMetricValueFloat <= threshold
		}
		lowerIsBetter := primaryScore.LowerIsBetter
		return &api.BenchmarkTest{
			PrimaryScore:       primaryMetricValueFloat,
			PrimaryScoreMetric: primaryScore.Metric,
			LowerIsBetter:      &lowerIsBetter,
			Threshold:          threshold,
			Pass:               pass,
		}
	}
	return nil
}

// selectPrimaryScore returns the first candidate whose metric key is present in
// metrics and whose value casts to float32. Non-numeric values are skipped so a
// lower-precedence candidate can still win.
func selectPrimaryScore(candidates []*api.PrimaryScore, metrics map[string]any) (*api.PrimaryScore, float32, bool) {
	for _, cand := range candidates {
		if cand == nil || cand.Metric == "" {
			continue
		}
		v, ok := metrics[cand.Metric]
		if !ok {
			continue
		}
		f, err := castAnyToFloat32(v)
		if err != nil {
			continue
		}
		return cand, f, true
	}
	return nil, 0, false
}

func primaryScoreMetricNames(candidates []*api.PrimaryScore) []string {
	names := make([]string, 0, len(candidates))
	for _, cand := range candidates {
		if cand != nil && cand.Metric != "" {
			names = append(names, cand.Metric)
		}
	}
	return names
}

func castAnyToFloat32(primaryMetricValue any) (float32, error) {
	var primaryMetricValueFloat float32
	switch v := primaryMetricValue.(type) {
	case float64:
		primaryMetricValueFloat = float32(v)
	case float32:
		primaryMetricValueFloat = v
	case int:
		primaryMetricValueFloat = float32(v)
	case int32:
		primaryMetricValueFloat = float32(v)
	case int64:
		primaryMetricValueFloat = float32(v)
	default:
		return 0, fmt.Errorf("unsupported type: %T for primary metric value", primaryMetricValue)
	}
	return primaryMetricValueFloat, nil
}
