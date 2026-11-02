package features

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// Keep the provider scenarios' result-count assertions in sync with the job
// payloads, so changing a benchmark selection cannot leave a stale expectation.
func TestBenchmarkProviderFixtureCounts(t *testing.T) {
	featurePath := filepath.Join(filepath.Dir(testDataRoot()), "evaluation_jobs_for_benchmark_providers.feature")
	feature, err := os.ReadFile(featurePath)
	if err != nil {
		t.Fatal(err)
	}
	scenarios := strings.Split(string(feature), "\n  Scenario: ")
	if len(scenarios) < 2 {
		t.Fatal("no benchmark provider scenarios found")
	}
	jobStep := regexp.MustCompile(`(?m)^\s*When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/([^"]+)"$`)
	countStep := regexp.MustCompile(`(?m)^\s*And the array at path "results.benchmarks" in the response should have length (\d+)$`)

	for _, scenario := range scenarios[1:] {
		name, _, _ := strings.Cut(scenario, "\n")
		t.Run(name, func(t *testing.T) {
			job := jobStep.FindStringSubmatch(scenario)
			count := countStep.FindStringSubmatch(scenario)
			if len(job) == 0 && len(count) == 0 {
				t.Skip("scenario does not submit a job fixture")
			}
			if len(job) != 2 || len(count) != 2 {
				t.Fatal("scenario must submit a job fixture and assert its benchmark result count")
			}
			want, err := strconv.Atoi(count[1])
			if err != nil {
				t.Fatal(err)
			}
			fixture := (&scenarioConfig{}).jsonnetSiblingName(job[1])
			for _, mode := range []string{"connected", "disconnected"} {
				t.Run(mode, func(t *testing.T) {
					mlflowOff := false
					tc := &scenarioConfig{
						values: map[string]string{},
						jsonnetHarnessEnv: map[string]string{
							"ENVIRONMENT_ID":        mode,
							"RAGAS_EMBEDDING_MODEL": "test-embedding-model",
						},
						jsonnetMlflowEnabled: &mlflowOff,
					}
					doc := evaluateJsonnetPayloadDocument(t, tc, fixture)
					if got := len(doc.Benchmarks); got != want {
						t.Errorf("%s submits %d benchmarks, but the feature expects %d results", fixture, got, want)
					}
				})
			}
		})
	}
}
