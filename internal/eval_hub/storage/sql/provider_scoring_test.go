package sql_test

import (
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/common"
	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
	"github.com/eval-hub/eval-hub/internal/logging"
	"github.com/eval-hub/eval-hub/internal/testhelpers"
	"github.com/eval-hub/eval-hub/pkg/api"
)

func TestDeepEvalHallucinationPassCriteria(t *testing.T) {
	const (
		providerID  = "deepeval"
		benchmarkID = "deepeval-hallucination"
		metric      = "hallucination_score"
	)
	providers, err := config.LoadProviderConfigs(logging.FallbackLogger(), testhelpers.NewValidator(t), "../../../../config")
	if err != nil {
		t.Fatal(err)
	}
	provider, ok := providers[providerID]
	if !ok {
		t.Fatalf("built-in %q provider was not loaded", providerID)
	}
	store, err := getTestStorage(t, "sqlite", getDBName())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.LoadSystemResources(nil, map[string]api.ProviderResource{providerID: provider}); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name  string
		score float32
		pass  bool
	}{
		{name: "above_cutoff", score: 0.667, pass: true},
		{name: "at_cutoff", score: 0.5, pass: true},
		{name: "fully_contradicted", score: 0, pass: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Leave scoring metadata out of the job and event so the server uses the catalog defaults.
			jobID := common.GUID()
			job := &api.EvaluationJobResource{
				Resource: api.EvaluationResource{Resource: api.Resource{ID: jobID}},
				EvaluationJobConfig: api.EvaluationJobConfig{
					Model: &api.ModelRef{Name: "probe", URL: "http://model.example/v1"},
					Benchmarks: []api.EvaluationBenchmarkConfig{
						{Ref: api.Ref{ID: benchmarkID}, ProviderID: providerID},
					},
				},
				Status: &api.EvaluationJobStatus{EvaluationJobState: api.EvaluationJobState{State: api.OverallStatePending}},
			}
			if err := store.CreateEvaluationJob(job); err != nil {
				t.Fatal(err)
			}
			event := &api.StatusEvent{BenchmarkStatusEvent: &api.BenchmarkStatusEvent{
				ID:         benchmarkID,
				ProviderID: providerID,
				Status:     api.StateCompleted,
				Metrics:    map[string]any{metric: tc.score},
			}}
			if err := store.UpdateEvaluationJob(jobID, event); err != nil {
				t.Fatal(err)
			}
			persisted, err := store.GetEvaluationJob(jobID)
			if err != nil {
				t.Fatal(err)
			}
			if persisted.Results == nil || len(persisted.Results.Benchmarks) != 1 || persisted.Results.Benchmarks[0].Test == nil {
				t.Fatalf("expected one benchmark test result, got %+v", persisted.Results)
			}
			result := persisted.Results.Benchmarks[0].Test
			if result.Pass != tc.pass {
				t.Errorf("benchmark pass = %t for score %g, want %t", result.Pass, tc.score, tc.pass)
			}
			if result.PrimaryScoreMetric != metric || result.PrimaryScore != tc.score ||
				result.LowerIsBetter == nil || *result.LowerIsBetter || result.Threshold != 0.5 {
				t.Errorf("benchmark test = %+v, want %s=%g, higher-is-better, threshold 0.5", result, metric, tc.score)
			}
		})
	}
}
