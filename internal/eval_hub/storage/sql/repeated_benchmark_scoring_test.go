package sql_test

import (
	"math"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/common"
	"github.com/eval-hub/eval-hub/pkg/api"
)

func TestRepeatedBenchmarkScoringUsesIndex(t *testing.T) {
	for _, collectionMode := range []bool{false, true} {
		mode := "benchmarks"
		if collectionMode {
			mode = "collection"
		}
		for _, order := range []struct {
			name    string
			indices [2]int
		}{
			{"in-order", [2]int{0, 1}},
			{"reverse-order", [2]int{1, 0}},
		} {
			for _, tc := range []struct {
				name          string
				primary       [2]api.PrimaryScore
				thresholds    [2]float32
				metrics       map[string]any
				wantScores    [2]float32
				wantPass      [2]bool
				wantAggregate float32
			}{
				{
					name:       "threshold",
					primary:    [2]api.PrimaryScore{{Metric: "acc"}, {Metric: "acc"}},
					thresholds: [2]float32{0.40, 0.90}, metrics: map[string]any{"acc": 0.70},
					wantScores: [2]float32{0.70, 0.70}, wantPass: [2]bool{true, false}, wantAggregate: 0.70,
				},
				{
					name:       "metric",
					primary:    [2]api.PrimaryScore{{Metric: "call_accuracy"}, {Metric: "relevance_accuracy"}},
					thresholds: [2]float32{0.60, 0.60}, metrics: map[string]any{"call_accuracy": 0.95, "relevance_accuracy": 0.20},
					wantScores: [2]float32{0.95, 0.20}, wantPass: [2]bool{true, false}, wantAggregate: 0.3875,
				},
				{
					name:       "direction",
					primary:    [2]api.PrimaryScore{{Metric: "error_rate"}, {Metric: "error_rate", LowerIsBetter: true}},
					thresholds: [2]float32{0.40, 0.40}, metrics: map[string]any{"error_rate": 0.20},
					wantScores: [2]float32{0.20, 0.20}, wantPass: [2]bool{false, true}, wantAggregate: 0.65,
				},
			} {
				t.Run(mode+"/"+order.name+"/"+tc.name, func(t *testing.T) {
					store, err := getTestStorage(t, "sqlite", getDBName())
					if err != nil {
						t.Fatal(err)
					}
					var rows []api.CollectionBenchmarkConfig
					for index := range 2 {
						rows = append(rows, api.CollectionBenchmarkConfig{
							Ref: api.Ref{ID: "repeated"}, ProviderID: "probe", Weight: float32(1 + 2*index),
							PrimaryScore: &tc.primary[index], PassCriteria: &api.PassCriteria{Threshold: &tc.thresholds[index]},
						})
					}
					job := &api.EvaluationJobResource{
						Resource:            api.EvaluationResource{Resource: api.Resource{ID: common.GUID(), CreatedAt: time.Now()}},
						EvaluationJobConfig: api.EvaluationJobConfig{Model: &api.ModelRef{Name: "probe", URL: "http://model.example/v1"}},
						Status:              &api.EvaluationJobStatus{EvaluationJobState: api.EvaluationJobState{State: api.OverallStatePending}},
					}
					if collectionMode {
						collection := &api.CollectionResource{
							Resource:         api.Resource{ID: common.GUID(), CreatedAt: time.Now()},
							CollectionConfig: api.CollectionConfig{Name: "Repeated benchmark scoring", Benchmarks: rows},
						}
						if err := store.CreateCollection(collection); err != nil {
							t.Fatal(err)
						}
						job.Collection = &api.CollectionRef{ID: collection.Resource.ID}
					} else {
						for _, row := range rows {
							job.Benchmarks = append(job.Benchmarks, row.ToEvaluationBenchmark())
						}
					}
					if err := store.CreateEvaluationJob(job); err != nil {
						t.Fatal(err)
					}
					for _, index := range order.indices {
						if err := store.UpdateEvaluationJob(job.Resource.ID, &api.StatusEvent{BenchmarkStatusEvent: &api.BenchmarkStatusEvent{
							ID: "repeated", ProviderID: "probe", BenchmarkIndex: index, Status: api.StateCompleted, Metrics: tc.metrics,
						}}); err != nil {
							t.Fatal(err)
						}
					}
					persisted, err := store.GetEvaluationJob(job.Resource.ID)
					if err != nil {
						t.Fatal(err)
					}
					if persisted.Results == nil || len(persisted.Results.Benchmarks) != 2 {
						t.Fatalf("expected two distinct persisted results, got %+v", persisted.Results)
					}
					for _, result := range persisted.Results.Benchmarks {
						index := result.BenchmarkIndex
						if index < 0 || index >= len(rows) || result.Test == nil || result.Test.LowerIsBetter == nil {
							t.Fatalf("invalid persisted benchmark result: %+v", result)
						}
						got := result.Test
						if got.PrimaryScoreMetric != tc.primary[index].Metric || got.PrimaryScore != tc.wantScores[index] ||
							*got.LowerIsBetter != tc.primary[index].LowerIsBetter || got.Threshold != tc.thresholds[index] || got.Pass != tc.wantPass[index] {
							t.Errorf("index %d: got %+v; want metric=%s score=%v lower=%t threshold=%v pass=%t", index,
								got, tc.primary[index].Metric, tc.wantScores[index], tc.primary[index].LowerIsBetter, tc.thresholds[index], tc.wantPass[index])
						}
					}
					if persisted.Status.State != api.OverallStateCompleted || persisted.Results.Test == nil {
						t.Fatalf("job did not complete with an aggregate: %+v", persisted)
					}
					if got := persisted.Results.Test; math.Abs(float64(got.Score-tc.wantAggregate)) > 1e-6 || got.Pass != (tc.wantAggregate >= 0.5) {
						t.Errorf("aggregate = %+v; want score=%v pass=%t", got, tc.wantAggregate, tc.wantAggregate >= 0.5)
					}
				})
			}
		}
	}
}
