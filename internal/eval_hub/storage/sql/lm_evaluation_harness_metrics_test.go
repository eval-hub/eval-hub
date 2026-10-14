package sql

import (
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
	"github.com/eval-hub/eval-hub/internal/testhelpers"
	"github.com/eval-hub/eval-hub/pkg/api"
)

// The fixture's callback metrics were produced by the stable LMES adapter's
// extraction functions using synthetic lm-eval results. Test the bundled provider
// configuration against those callback names, rather than raw lm-eval keys.
func TestComputeBenchmarkTestResult_BBHAdapterMetrics(t *testing.T) {
	data, err := os.ReadFile("testdata/lm_evaluation_harness_bbh_metrics.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures struct {
		Cases []struct {
			BenchmarkIDs    []string       `json:"benchmark_ids"`
			CallbackMetrics map[string]any `json:"callback_metrics"`
			ExpectedScore   float32        `json:"expected_score"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	if len(fixtures.Cases) == 0 {
		t.Fatal("no adapter metric fixtures")
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	providers, err := config.LoadProviderConfigs(logger, testhelpers.NewValidator(t), "../../../../config")
	if err != nil {
		t.Fatalf("load provider configs: %v", err)
	}
	const providerID = "lm_evaluation_harness"
	provider, ok := providers[providerID]
	if !ok {
		t.Fatal("lm_evaluation_harness provider not found")
	}
	benchmarks := make(map[string]api.BenchmarkResource, len(provider.Benchmarks))
	for _, benchmark := range provider.Benchmarks {
		benchmarks[benchmark.ID] = benchmark
	}
	for _, fixture := range fixtures.Cases {
		if len(fixture.BenchmarkIDs) == 0 {
			t.Fatal("adapter metric fixture has no benchmarks")
		}
		for _, id := range fixture.BenchmarkIDs {
			t.Run(id, func(t *testing.T) {
				benchmark, ok := benchmarks[id]
				if !ok {
					t.Fatalf("benchmark %s not found in bundled provider", id)
				}
				if benchmark.PrimaryScore == nil || benchmark.PassCriteria == nil || benchmark.PassCriteria.Threshold == nil {
					t.Fatal("benchmark primary score or threshold missing")
				}
				job := jobWithBenchmark(id, providerID, benchmark.PrimaryScore, benchmark.PassCriteria)
				event := statusEvent(id, providerID, fixture.CallbackMetrics)
				result := testResultsStorage().computeBenchmarkTestResult(nil, job, event, nil)
				if result == nil {
					t.Fatalf("callback metrics %v do not match configured primary metric %q", fixture.CallbackMetrics, benchmark.PrimaryScore.Metric)
				}
				if result.PrimaryScoreMetric != "exact_match" || result.PrimaryScore != fixture.ExpectedScore || result.Threshold != *benchmark.PassCriteria.Threshold || !result.Pass {
					t.Errorf("unexpected test result for normalized adapter metrics: %+v", result)
				}
			})
		}
	}
}
