package local

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/handlers"
	"github.com/eval-hub/eval-hub/pkg/api"
)

func TestGetEvaluationLogsSingleBenchmark(t *testing.T) {
	providerID := "provider-1"
	jobID := "job-logs-1"
	evaluation := sampleEvaluation(providerID)
	evaluation.Resource.ID = jobID
	dirName := localJobDir(jobID, 0, providerID, "bench-1")
	cleanupDir(t, "job-logs-1")

	if err := os.MkdirAll(dirName, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	logPath := filepath.Join(dirName, "jobrun.log")
	if err := os.WriteFile(logPath, []byte("line1\nline2\n"), 0644); err != nil {
		t.Fatalf("write log: %v", err)
	}

	rt := &LocalRuntime{logger: discardLogger(), ctx: context.Background()}
	benchmarks, err := handlers.GetJobBenchmarks(evaluation, nil)
	if err != nil {
		t.Fatalf("GetJobBenchmarks: %v", err)
	}

	idx := 0
	got, err := rt.GetEvaluationLogs(evaluation, benchmarks, &idx, api.EvaluationLogOptions{TailLines: 10})
	if err != nil {
		t.Fatalf("GetEvaluationLogs: %v", err)
	}
	if got != "line1\nline2" {
		t.Fatalf("got %q, want %q", got, "line1\nline2")
	}
}

func TestGetEvaluationLogsAllBenchmarks(t *testing.T) {
	providerID := "provider-1"
	evaluation := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{
			Resource: api.Resource{ID: "job-logs-2", Tenant: "tenant"},
		},
		EvaluationJobConfig: api.EvaluationJobConfig{
			Benchmarks: []api.EvaluationBenchmarkConfig{
				{Ref: api.Ref{ID: "bench-1"}, ProviderID: providerID},
				{Ref: api.Ref{ID: "bench-2"}, ProviderID: providerID},
			},
		},
	}

	for i, benchID := range []string{"bench-1", "bench-2"} {
		dirName := localJobDir("job-logs-2", i, providerID, benchID)
		cleanupDir(t, "job-logs-2")
		if err := os.MkdirAll(dirName, 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dirName, "jobrun.log"), []byte(fmt.Sprintf("log-%d\n", i)), 0644); err != nil {
			t.Fatalf("write log: %v", err)
		}
	}

	rt := &LocalRuntime{logger: discardLogger(), ctx: context.Background()}
	benchmarks, err := handlers.GetJobBenchmarks(evaluation, nil)
	if err != nil {
		t.Fatalf("GetJobBenchmarks: %v", err)
	}

	got, err := rt.GetEvaluationLogs(evaluation, benchmarks, nil, api.EvaluationLogOptions{TailLines: 10})
	if err != nil {
		t.Fatalf("GetEvaluationLogs: %v", err)
	}
	if want := "=== pod=job-logs-2-0 container=local benchmark_id=bench-1 ===\nlog-0\n=== pod=job-logs-2-1 container=local benchmark_id=bench-2 ===\nlog-1"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestGetEvaluationLogsInvalidBenchmarkIndex(t *testing.T) {
	providerID := "provider-1"
	evaluation := sampleEvaluation(providerID)
	rt := &LocalRuntime{logger: discardLogger(), ctx: context.Background()}
	benchmarks, err := handlers.GetJobBenchmarks(evaluation, nil)
	if err != nil {
		t.Fatalf("GetJobBenchmarks: %v", err)
	}

	idx := 3
	_, err = rt.GetEvaluationLogs(evaluation, benchmarks, &idx, api.EvaluationLogOptions{TailLines: 10})
	if err == nil {
		t.Fatal("expected error for out-of-range benchmark index")
	}
}
