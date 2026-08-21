package k8s

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/handlers"
	"github.com/eval-hub/eval-hub/pkg/api"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestStreamEvaluationLogsReturnsAdapterLogs(t *testing.T) {
	evaluation := sampleEvaluation("provider-1")
	jobID := evaluation.Resource.ID
	namespace := "default"
	jobName := "eval-job-logs"
	podName := "eval-pod-logs"

	clientset := fake.NewClientset(
		&batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:      jobName,
				Namespace: namespace,
				Labels: map[string]string{
					labelJobIDKey:          sanitizeLabelValue(jobID),
					labelBenchmarkIndexKey: "0",
				},
			},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      podName,
				Namespace: namespace,
				Labels: map[string]string{
					"job-name": jobName,
				},
			},
		},
	)

	runtime := &K8sRuntime{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		helper: &KubernetesHelper{clientset: clientset},
		ctx:    context.Background(),
	}

	benchmarks, err := handlers.GetJobBenchmarks(evaluation, nil)
	if err != nil {
		t.Fatalf("GetJobBenchmarks: %v", err)
	}

	var buf bytes.Buffer
	idx := 0
	err = runtime.StreamEvaluationLogs(evaluation, benchmarks, &idx, api.EvaluationLogOptions{TailLines: 10}, &buf)
	if err != nil {
		t.Fatalf("StreamEvaluationLogs: %v", err)
	}
	if got := buf.String(); got != "fake logs" {
		t.Fatalf("got %q, want %q", got, "fake logs")
	}
}

func TestStreamEvaluationLogsAllBenchmarkSections(t *testing.T) {
	evaluation := sampleEvaluation("provider-1")
	namespace := "default"
	jobName := "eval-job-logs-0"

	clientset := fake.NewClientset(
		&batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:      jobName,
				Namespace: namespace,
				Labels: map[string]string{
					labelJobIDKey:          sanitizeLabelValue(evaluation.Resource.ID),
					labelBenchmarkIndexKey: "0",
					labelBenchmarkIDKey:    "bench-1",
				},
			},
		},
	)

	runtime := &K8sRuntime{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		helper: &KubernetesHelper{clientset: clientset},
		ctx:    context.Background(),
	}

	benchmarks, err := handlers.GetJobBenchmarks(evaluation, nil)
	if err != nil {
		t.Fatalf("GetJobBenchmarks: %v", err)
	}

	var buf bytes.Buffer
	err = runtime.StreamEvaluationLogs(evaluation, benchmarks, nil, api.EvaluationLogOptions{TailLines: 10}, &buf)
	if err != nil {
		t.Fatalf("StreamEvaluationLogs: %v", err)
	}
	want := fmt.Sprintf("=== pod=%s container=%s benchmark_id=bench-1 ===", jobName, adapterContainerName)
	if got := buf.String(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestStreamEvaluationLogsSectionWithPodLogContent(t *testing.T) {
	evaluation := sampleEvaluation("provider-1")
	namespace := "default"
	jobName := "eval-job-logs-full"
	podName := "eval-pod-logs-full"

	clientset := fake.NewClientset(
		&batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:      jobName,
				Namespace: namespace,
				Labels: map[string]string{
					labelJobIDKey:          sanitizeLabelValue(evaluation.Resource.ID),
					labelBenchmarkIndexKey: "0",
					labelBenchmarkIDKey:    "bench-1",
				},
			},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      podName,
				Namespace: namespace,
				Labels: map[string]string{
					"job-name": jobName,
				},
			},
		},
	)

	since := 60
	runtime := &K8sRuntime{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		helper: &KubernetesHelper{clientset: clientset},
		ctx:    context.Background(),
	}

	benchmarks, err := handlers.GetJobBenchmarks(evaluation, nil)
	if err != nil {
		t.Fatalf("GetJobBenchmarks: %v", err)
	}

	var buf bytes.Buffer
	err = runtime.StreamEvaluationLogs(evaluation, benchmarks, nil, api.EvaluationLogOptions{
		TailLines:    25,
		Timestamps:   true,
		SinceSeconds: &since,
	}, &buf)
	if err != nil {
		t.Fatalf("StreamEvaluationLogs: %v", err)
	}
	want := fmt.Sprintf("=== pod=%s container=%s benchmark_id=bench-1 ===\nfake logs", podName, adapterContainerName)
	if got := buf.String(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestStreamEvaluationLogsSingleBenchmarkNoJob(t *testing.T) {
	evaluation := sampleEvaluation("provider-1")
	runtime := &K8sRuntime{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		helper: &KubernetesHelper{clientset: fake.NewClientset()},
		ctx:    context.Background(),
	}

	benchmarks, err := handlers.GetJobBenchmarks(evaluation, nil)
	if err != nil {
		t.Fatalf("GetJobBenchmarks: %v", err)
	}

	var buf bytes.Buffer
	idx := 0
	err = runtime.StreamEvaluationLogs(evaluation, benchmarks, &idx, api.EvaluationLogOptions{TailLines: 10}, &buf)
	if err != nil {
		t.Fatalf("StreamEvaluationLogs: %v", err)
	}
	if got := buf.String(); got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

func TestStreamEvaluationLogsSingleBenchmarkNoPod(t *testing.T) {
	evaluation := sampleEvaluation("provider-1")
	jobID := evaluation.Resource.ID
	namespace := "default"

	clientset := fake.NewClientset(
		&batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "job-no-pod",
				Namespace: namespace,
				Labels: map[string]string{
					labelJobIDKey:          sanitizeLabelValue(jobID),
					labelBenchmarkIndexKey: "0",
				},
			},
		},
	)

	runtime := &K8sRuntime{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		helper: &KubernetesHelper{clientset: clientset},
		ctx:    context.Background(),
	}

	benchmarks, err := handlers.GetJobBenchmarks(evaluation, nil)
	if err != nil {
		t.Fatalf("GetJobBenchmarks: %v", err)
	}

	var buf bytes.Buffer
	idx := 0
	err = runtime.StreamEvaluationLogs(evaluation, benchmarks, &idx, api.EvaluationLogOptions{TailLines: 10}, &buf)
	if err != nil {
		t.Fatalf("StreamEvaluationLogs: %v", err)
	}
	if got := buf.String(); got != "" {
		t.Fatalf("got %q, want empty (no pod found, single benchmark = no header)", got)
	}
}

func TestStreamEvaluationLogsRequiresContext(t *testing.T) {
	evaluation := sampleEvaluation("provider-1")
	runtime := &K8sRuntime{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		helper: &KubernetesHelper{clientset: fake.NewClientset()},
	}

	var buf bytes.Buffer
	err := runtime.StreamEvaluationLogs(evaluation, evaluation.Benchmarks, nil, api.EvaluationLogOptions{TailLines: 10}, &buf)
	if err == nil {
		t.Fatal("expected error for nil context")
	}
}

func TestStreamEvaluationLogsRejectsEmptyBenchmarks(t *testing.T) {
	evaluation := sampleEvaluation("provider-1")
	runtime := &K8sRuntime{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		helper: &KubernetesHelper{clientset: fake.NewClientset()},
		ctx:    context.Background(),
	}

	var buf bytes.Buffer
	err := runtime.StreamEvaluationLogs(evaluation, nil, nil, api.EvaluationLogOptions{TailLines: 10}, &buf)
	if err == nil {
		t.Fatal("expected error for empty benchmarks")
	}
}

func TestStreamEvaluationLogsRejectsNegativeBenchmarkIndex(t *testing.T) {
	evaluation := sampleEvaluation("provider-1")
	runtime := &K8sRuntime{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		helper: &KubernetesHelper{clientset: fake.NewClientset()},
		ctx:    context.Background(),
	}

	benchmarks, err := handlers.GetJobBenchmarks(evaluation, nil)
	if err != nil {
		t.Fatalf("GetJobBenchmarks: %v", err)
	}

	var buf bytes.Buffer
	idx := -1
	err = runtime.StreamEvaluationLogs(evaluation, benchmarks, &idx, api.EvaluationLogOptions{TailLines: 10}, &buf)
	if err == nil {
		t.Fatal("expected error for negative benchmark index")
	}
}

func TestStreamEvaluationLogsAllBenchmarkNoJobHeader(t *testing.T) {
	evaluation := sampleEvaluation("provider-1")
	runtime := &K8sRuntime{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		helper: &KubernetesHelper{clientset: fake.NewClientset()},
		ctx:    context.Background(),
	}

	benchmarks, err := handlers.GetJobBenchmarks(evaluation, nil)
	if err != nil {
		t.Fatalf("GetJobBenchmarks: %v", err)
	}

	var buf bytes.Buffer
	err = runtime.StreamEvaluationLogs(evaluation, benchmarks, nil, api.EvaluationLogOptions{TailLines: 10}, &buf)
	if err != nil {
		t.Fatalf("StreamEvaluationLogs: %v", err)
	}
	want := fmt.Sprintf("=== pod=unknown container=%s benchmark_id=bench-1 ===", adapterContainerName)
	if got := buf.String(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestStreamEvaluationLogsJobWithNoPodHeader(t *testing.T) {
	evaluation := sampleEvaluation("provider-1")
	namespace := "default"
	jobName := "eval-job-nopod"

	clientset := fake.NewClientset(
		&batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:      jobName,
				Namespace: namespace,
				Labels: map[string]string{
					labelJobIDKey:          sanitizeLabelValue(evaluation.Resource.ID),
					labelBenchmarkIndexKey: "0",
				},
			},
		},
	)

	runtime := &K8sRuntime{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		helper: &KubernetesHelper{clientset: clientset},
		ctx:    context.Background(),
	}

	benchmarks, err := handlers.GetJobBenchmarks(evaluation, nil)
	if err != nil {
		t.Fatalf("GetJobBenchmarks: %v", err)
	}

	var buf bytes.Buffer
	err = runtime.StreamEvaluationLogs(evaluation, benchmarks, nil, api.EvaluationLogOptions{TailLines: 10}, &buf)
	if err != nil {
		t.Fatalf("StreamEvaluationLogs: %v", err)
	}
	want := fmt.Sprintf("=== pod=%s container=%s benchmark_id=bench-1 ===", jobName, adapterContainerName)
	if got := buf.String(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestStreamEvaluationLogsMultiBenchmarkSeparator(t *testing.T) {
	evaluation := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{
			Resource: api.Resource{ID: "job-multi"},
		},
		EvaluationJobConfig: api.EvaluationJobConfig{
			Benchmarks: []api.EvaluationBenchmarkConfig{
				{Ref: api.Ref{ID: "bench-1"}, ProviderID: "provider-1"},
				{Ref: api.Ref{ID: "bench-2"}, ProviderID: "provider-1"},
			},
		},
	}
	runtime := &K8sRuntime{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		helper: &KubernetesHelper{clientset: fake.NewClientset()},
		ctx:    context.Background(),
	}

	benchmarks, err := handlers.GetJobBenchmarks(evaluation, nil)
	if err != nil {
		t.Fatalf("GetJobBenchmarks: %v", err)
	}

	var buf bytes.Buffer
	err = runtime.StreamEvaluationLogs(evaluation, benchmarks, nil, api.EvaluationLogOptions{TailLines: 10}, &buf)
	if err != nil {
		t.Fatalf("StreamEvaluationLogs: %v", err)
	}
	got := buf.String()
	header0 := fmt.Sprintf("=== pod=unknown container=%s benchmark_id=bench-1 ===", adapterContainerName)
	header1 := fmt.Sprintf("=== pod=unknown container=%s benchmark_id=bench-2 ===", adapterContainerName)
	want := header0 + "\n" + header1
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestStreamEvaluationLogsMultiBenchmarkWithJobs(t *testing.T) {
	evaluation := &api.EvaluationJobResource{
		Resource: api.EvaluationResource{
			Resource: api.Resource{ID: "multi-bench-job", Tenant: "default"},
		},
		EvaluationJobConfig: api.EvaluationJobConfig{
			Benchmarks: []api.EvaluationBenchmarkConfig{
				{Ref: api.Ref{ID: "bench-1"}, ProviderID: "provider-1"},
				{Ref: api.Ref{ID: "bench-2"}, ProviderID: "provider-1"},
			},
		},
	}
	namespace := "default"

	clientset := fake.NewClientset(
		&batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "job-0",
				Namespace: namespace,
				Labels: map[string]string{
					labelJobIDKey:          sanitizeLabelValue(evaluation.Resource.ID),
					labelBenchmarkIndexKey: "0",
				},
			},
		},
		&batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "job-1",
				Namespace: namespace,
				Labels: map[string]string{
					labelJobIDKey:          sanitizeLabelValue(evaluation.Resource.ID),
					labelBenchmarkIndexKey: "1",
				},
			},
		},
	)

	runtime := &K8sRuntime{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		helper: &KubernetesHelper{clientset: clientset},
		ctx:    context.Background(),
	}

	benchmarks, err := handlers.GetJobBenchmarks(evaluation, nil)
	if err != nil {
		t.Fatalf("GetJobBenchmarks: %v", err)
	}

	var buf bytes.Buffer
	err = runtime.StreamEvaluationLogs(evaluation, benchmarks, nil, api.EvaluationLogOptions{TailLines: 10}, &buf)
	if err != nil {
		t.Fatalf("StreamEvaluationLogs: %v", err)
	}
	got := buf.String()
	if !strings.Contains(got, "benchmark_id=bench-1") || !strings.Contains(got, "benchmark_id=bench-2") {
		t.Fatalf("expected both benchmark headers, got %q", got)
	}
	parts := strings.Split(got, "\n")
	if len(parts) < 2 {
		t.Fatalf("expected newline separator between benchmarks, got %q", got)
	}
}

func TestStreamEvaluationLogsOutOfRangeIndex(t *testing.T) {
	evaluation := sampleEvaluation("provider-1")
	runtime := &K8sRuntime{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		helper: &KubernetesHelper{clientset: fake.NewClientset()},
		ctx:    context.Background(),
	}

	benchmarks, err := handlers.GetJobBenchmarks(evaluation, nil)
	if err != nil {
		t.Fatalf("GetJobBenchmarks: %v", err)
	}

	var buf bytes.Buffer
	idx := 5
	err = runtime.StreamEvaluationLogs(evaluation, benchmarks, &idx, api.EvaluationLogOptions{TailLines: 10}, &buf)
	if err == nil {
		t.Fatal("expected error for out-of-range benchmark index")
	}
}

func TestStreamEvaluationLogsTailLinesAllLines(t *testing.T) {
	evaluation := sampleEvaluation("provider-1")
	jobID := evaluation.Resource.ID
	namespace := "default"
	jobName := "eval-job-alllines"
	podName := "eval-pod-alllines"

	clientset := fake.NewClientset(
		&batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:      jobName,
				Namespace: namespace,
				Labels: map[string]string{
					labelJobIDKey:          sanitizeLabelValue(jobID),
					labelBenchmarkIndexKey: "0",
				},
			},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      podName,
				Namespace: namespace,
				Labels:    map[string]string{"job-name": jobName},
			},
		},
	)

	runtime := &K8sRuntime{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		helper: &KubernetesHelper{clientset: clientset},
		ctx:    context.Background(),
	}

	benchmarks, err := handlers.GetJobBenchmarks(evaluation, nil)
	if err != nil {
		t.Fatalf("GetJobBenchmarks: %v", err)
	}

	var buf bytes.Buffer
	idx := 0
	err = runtime.StreamEvaluationLogs(evaluation, benchmarks, &idx, api.EvaluationLogOptions{TailLines: api.AllLogLines}, &buf)
	if err != nil {
		t.Fatalf("StreamEvaluationLogs: %v", err)
	}
	if got := buf.String(); got != "fake logs" {
		t.Fatalf("got %q, want %q", got, "fake logs")
	}
}

func TestStreamEvaluationLogsWithSinceSeconds(t *testing.T) {
	evaluation := sampleEvaluation("provider-1")
	jobID := evaluation.Resource.ID
	namespace := "default"
	jobName := "eval-job-since"
	podName := "eval-pod-since"

	clientset := fake.NewClientset(
		&batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:      jobName,
				Namespace: namespace,
				Labels: map[string]string{
					labelJobIDKey:          sanitizeLabelValue(jobID),
					labelBenchmarkIndexKey: "0",
				},
			},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      podName,
				Namespace: namespace,
				Labels:    map[string]string{"job-name": jobName},
			},
		},
	)

	runtime := &K8sRuntime{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		helper: &KubernetesHelper{clientset: clientset},
		ctx:    context.Background(),
	}

	benchmarks, err := handlers.GetJobBenchmarks(evaluation, nil)
	if err != nil {
		t.Fatalf("GetJobBenchmarks: %v", err)
	}

	sinceSeconds := 60
	var buf bytes.Buffer
	idx := 0
	err = runtime.StreamEvaluationLogs(evaluation, benchmarks, &idx, api.EvaluationLogOptions{
		TailLines:    10,
		SinceSeconds: &sinceSeconds,
		Timestamps:   true,
	}, &buf)
	if err != nil {
		t.Fatalf("StreamEvaluationLogs: %v", err)
	}
	if got := buf.String(); got != "fake logs" {
		t.Fatalf("got %q, want %q", got, "fake logs")
	}
}

func TestLatestJobPodSelectsNewestPod(t *testing.T) {
	namespace := "default"
	jobName := "eval-job-pods"
	older := metav1.NewTime(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	newer := metav1.NewTime(time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC))

	clientset := fake.NewClientset(
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "older-pod",
				Namespace:         namespace,
				Labels:            map[string]string{"job-name": jobName},
				CreationTimestamp: older,
			},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "newer-pod",
				Namespace:         namespace,
				Labels:            map[string]string{"job-name": jobName},
				CreationTimestamp: newer,
			},
		},
	)

	runtime := &K8sRuntime{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		helper: &KubernetesHelper{clientset: clientset},
		ctx:    context.Background(),
	}

	pod, err := runtime.latestJobPod(namespace, jobName)
	if err != nil {
		t.Fatalf("latestJobPod: %v", err)
	}
	if pod == nil || pod.Name != "newer-pod" {
		t.Fatalf("pod = %v, want newer-pod", pod)
	}
}
