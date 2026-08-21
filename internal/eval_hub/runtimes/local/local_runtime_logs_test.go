package local

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/handlers"
	"github.com/eval-hub/eval-hub/pkg/api"
)

func TestStreamEvaluationLogsSingleBenchmark(t *testing.T) {
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

	var buf bytes.Buffer
	idx := 0
	err = rt.StreamEvaluationLogs(evaluation, benchmarks, &idx, api.EvaluationLogOptions{TailLines: 10}, &buf)
	if err != nil {
		t.Fatalf("StreamEvaluationLogs: %v", err)
	}
	if got := buf.String(); got != "line1\nline2" {
		t.Fatalf("got %q, want %q", got, "line1\nline2")
	}
}

func TestStreamEvaluationLogsAllBenchmarks(t *testing.T) {
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

	var buf bytes.Buffer
	err = rt.StreamEvaluationLogs(evaluation, benchmarks, nil, api.EvaluationLogOptions{TailLines: 10}, &buf)
	if err != nil {
		t.Fatalf("StreamEvaluationLogs: %v", err)
	}
	want := "=== pod=job-logs-2-0 container=local benchmark_id=bench-1 ===\nlog-0\n=== pod=job-logs-2-1 container=local benchmark_id=bench-2 ===\nlog-1"
	if got := buf.String(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestStreamEvaluationLogsInvalidBenchmarkIndex(t *testing.T) {
	providerID := "provider-1"
	evaluation := sampleEvaluation(providerID)
	rt := &LocalRuntime{logger: discardLogger(), ctx: context.Background()}
	benchmarks, err := handlers.GetJobBenchmarks(evaluation, nil)
	if err != nil {
		t.Fatalf("GetJobBenchmarks: %v", err)
	}

	var buf bytes.Buffer
	idx := 3
	err = rt.StreamEvaluationLogs(evaluation, benchmarks, &idx, api.EvaluationLogOptions{TailLines: 10}, &buf)
	if err == nil {
		t.Fatal("expected error for out-of-range benchmark index")
	}
}

func TestStreamEvaluationLogsRequiresContext(t *testing.T) {
	providerID := "provider-1"
	evaluation := sampleEvaluation(providerID)
	rt := &LocalRuntime{logger: discardLogger()}
	var buf bytes.Buffer
	err := rt.StreamEvaluationLogs(evaluation, evaluation.Benchmarks, nil, api.EvaluationLogOptions{TailLines: 10}, &buf)
	if err == nil {
		t.Fatal("expected error for nil context")
	}
}

func TestStreamEvaluationLogsRejectsEmptyBenchmarks(t *testing.T) {
	providerID := "provider-1"
	evaluation := sampleEvaluation(providerID)
	rt := &LocalRuntime{logger: discardLogger(), ctx: context.Background()}
	var buf bytes.Buffer
	err := rt.StreamEvaluationLogs(evaluation, nil, nil, api.EvaluationLogOptions{TailLines: 10}, &buf)
	if err == nil {
		t.Fatal("expected error for empty benchmarks")
	}
}

func TestStreamEvaluationLogsTailLinesAllLines(t *testing.T) {
	providerID := "provider-1"
	jobID := "job-logs-alllines"
	evaluation := sampleEvaluation(providerID)
	evaluation.Resource.ID = jobID
	dirName := localJobDir(jobID, 0, providerID, "bench-1")
	cleanupDir(t, jobID)

	if err := os.MkdirAll(dirName, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	logPath := filepath.Join(dirName, "jobrun.log")
	if err := os.WriteFile(logPath, []byte("line1\nline2\nline3\n"), 0644); err != nil {
		t.Fatalf("write log: %v", err)
	}

	rt := &LocalRuntime{logger: discardLogger(), ctx: context.Background()}
	benchmarks, err := handlers.GetJobBenchmarks(evaluation, nil)
	if err != nil {
		t.Fatalf("GetJobBenchmarks: %v", err)
	}

	var buf bytes.Buffer
	idx := 0
	err = rt.StreamEvaluationLogs(evaluation, benchmarks, &idx, api.EvaluationLogOptions{TailLines: api.AllLogLines}, &buf)
	if err != nil {
		t.Fatalf("StreamEvaluationLogs: %v", err)
	}
	if got := buf.String(); got != "line1\nline2\nline3\n" {
		t.Fatalf("got %q, want %q", got, "line1\nline2\nline3\n")
	}
}

func TestStreamEvaluationLogsNegativeBenchmarkIndex(t *testing.T) {
	providerID := "provider-1"
	evaluation := sampleEvaluation(providerID)
	rt := &LocalRuntime{logger: discardLogger(), ctx: context.Background()}
	benchmarks, err := handlers.GetJobBenchmarks(evaluation, nil)
	if err != nil {
		t.Fatalf("GetJobBenchmarks: %v", err)
	}

	var buf bytes.Buffer
	idx := -1
	err = rt.StreamEvaluationLogs(evaluation, benchmarks, &idx, api.EvaluationLogOptions{TailLines: 10}, &buf)
	if err == nil {
		t.Fatal("expected error for negative benchmark index")
	}
}

func TestStreamEvaluationLogsHeaderOnlyWhenLogMissing(t *testing.T) {
	providerID := "provider-1"
	jobID := "job-logs-missing-file"
	evaluation := sampleEvaluation(providerID)
	evaluation.Resource.ID = jobID
	cleanupDir(t, jobID)

	rt := &LocalRuntime{logger: discardLogger(), ctx: context.Background()}
	benchmarks, err := handlers.GetJobBenchmarks(evaluation, nil)
	if err != nil {
		t.Fatalf("GetJobBenchmarks: %v", err)
	}

	var buf bytes.Buffer
	err = rt.StreamEvaluationLogs(evaluation, benchmarks, nil, api.EvaluationLogOptions{TailLines: 10}, &buf)
	if err != nil {
		t.Fatalf("StreamEvaluationLogs: %v", err)
	}
	want := "=== pod=job-logs-missing-file-0 container=local benchmark_id=bench-1 ===\n"
	if got := buf.String(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestStreamEvaluationLogsAllLines(t *testing.T) {
	providerID := "provider-1"
	jobID := "job-logs-all"
	evaluation := sampleEvaluation(providerID)
	evaluation.Resource.ID = jobID
	dirName := localJobDir(jobID, 0, providerID, "bench-1")
	cleanupDir(t, jobID)

	if err := os.MkdirAll(dirName, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := "line1\nline2\nline3\n"
	if err := os.WriteFile(filepath.Join(dirName, "jobrun.log"), []byte(content), 0644); err != nil {
		t.Fatalf("write log: %v", err)
	}

	rt := &LocalRuntime{logger: discardLogger(), ctx: context.Background()}
	benchmarks, err := handlers.GetJobBenchmarks(evaluation, nil)
	if err != nil {
		t.Fatalf("GetJobBenchmarks: %v", err)
	}

	var buf bytes.Buffer
	idx := 0
	err = rt.StreamEvaluationLogs(evaluation, benchmarks, &idx, api.EvaluationLogOptions{TailLines: api.AllLogLines}, &buf)
	if err != nil {
		t.Fatalf("StreamEvaluationLogs: %v", err)
	}
	if got := buf.String(); got != content {
		t.Fatalf("got %q, want %q", got, content)
	}
}

func TestStreamEvaluationLogsSingleBenchmarkNoHeader(t *testing.T) {
	providerID := "provider-1"
	jobID := "job-logs-no-header"
	evaluation := sampleEvaluation(providerID)
	evaluation.Resource.ID = jobID
	dirName := localJobDir(jobID, 0, providerID, "bench-1")
	cleanupDir(t, jobID)

	if err := os.MkdirAll(dirName, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dirName, "jobrun.log"), []byte("data\n"), 0644); err != nil {
		t.Fatalf("write log: %v", err)
	}

	rt := &LocalRuntime{logger: discardLogger(), ctx: context.Background()}
	benchmarks, err := handlers.GetJobBenchmarks(evaluation, nil)
	if err != nil {
		t.Fatalf("GetJobBenchmarks: %v", err)
	}

	var buf bytes.Buffer
	idx := 0
	err = rt.StreamEvaluationLogs(evaluation, benchmarks, &idx, api.EvaluationLogOptions{TailLines: 10}, &buf)
	if err != nil {
		t.Fatalf("StreamEvaluationLogs: %v", err)
	}
	if got := buf.String(); got != "data" {
		t.Fatalf("got %q, want %q (no header for single benchmark)", got, "data")
	}
}
