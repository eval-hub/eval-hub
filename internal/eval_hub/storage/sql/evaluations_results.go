package sql

import (
	"database/sql"
	"fmt"
	"reflect"

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
		weightedScore := benchmarkWeight * benchmark.Test.PrimaryScore
		if primaryScore := resolvedJobBenchmarks[benchmark.BenchmarkIndex].PrimaryScore; primaryScore != nil && primaryScore.LowerIsBetter {
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
		primaryScore := benchmark.PrimaryScore
		var providerBench *api.BenchmarkResource
		// if the primary score is not defined, we need to get the primary score from the provider
		if (primaryScore == nil || primaryScore.Metric == "") && benchmark.ProviderID != "" {
			provider, err := s.getUserProviderTransactional(txn, benchmark.ProviderID)
			if err == nil && provider != nil {
				for i := range provider.Benchmarks {
					if provider.Benchmarks[i].ID == benchmark.ID {
						providerBench = &provider.Benchmarks[i]
						break
					}
				}
			}
			if providerBench != nil && providerBench.PrimaryScore != nil && providerBench.PrimaryScore.Metric != "" {
				primaryScore = providerBench.PrimaryScore
			}
		}
		if primaryScore != nil && primaryScore.Metric != "" {
			primaryMetric := primaryScore.Metric
			primaryMetricValue, ok := benchmarkStatusEvent.Metrics[primaryMetric]
			if !ok {
				if len(benchmarkStatusEvent.Metrics) > 0 {
					s.logger.Error("Primary score metric not present in benchmark metrics; test section omitted",
						"benchmark_id", benchmarkStatusEvent.ID,
						"provider_id", benchmarkStatusEvent.ProviderID,
						"primary_metric", primaryMetric)
				}
				return nil
			}
			primaryMetricValueFloat, err := castAnyToFloat32(primaryMetricValue)
			if err != nil {
				s.logger.Error("Failed to cast primary metric value to float32", "error", err, "primary_metric", primaryMetric, "primary_metric_value", primaryMetricValue)
				return nil
			}
			var threshold float32
			if benchmark.PassCriteria != nil && benchmark.PassCriteria.Threshold != nil {
				threshold = *benchmark.PassCriteria.Threshold
			} else if providerBench != nil && providerBench.PassCriteria != nil && providerBench.PassCriteria.Threshold != nil {
				threshold = *providerBench.PassCriteria.Threshold
			} else {
				return nil
			}
			pass := primaryMetricValueFloat >= threshold
			if primaryScore.LowerIsBetter {
				pass = primaryMetricValueFloat <= threshold
			}
			return &api.BenchmarkTest{
				PrimaryScore:       primaryMetricValueFloat,
				PrimaryScoreMetric: primaryMetric,
				Threshold:          threshold,
				Pass:               pass,
			}
		}
	}
	return nil
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
