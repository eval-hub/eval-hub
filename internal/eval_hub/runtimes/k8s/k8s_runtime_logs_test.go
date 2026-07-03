package k8s

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/handlers"
	"github.com/eval-hub/eval-hub/pkg/api"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestGetEvaluationLogsReturnsAdapterLogs(t *testing.T) {
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

	idx := 0
	got, err := runtime.GetEvaluationLogs(evaluation, benchmarks, &idx, api.EvaluationLogOptions{TailLines: 10})
	if err != nil {
		t.Fatalf("GetEvaluationLogs: %v", err)
	}
	if got != "fake logs" {
		t.Fatalf("got %q, want %q", got, "fake logs")
	}
}

func TestGetEvaluationLogsAllBenchmarkSections(t *testing.T) {
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

	got, err := runtime.GetEvaluationLogs(evaluation, benchmarks, nil, api.EvaluationLogOptions{TailLines: 10})
	if err != nil {
		t.Fatalf("GetEvaluationLogs: %v", err)
	}
	want := fmt.Sprintf("=== pod=%s container=%s benchmark_id=bench-1 ===", jobName, adapterContainerName)
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestGetEvaluationLogsSectionWithPodLogContent(t *testing.T) {
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

	got, err := runtime.GetEvaluationLogs(evaluation, benchmarks, nil, api.EvaluationLogOptions{
		TailLines:    25,
		Timestamps:   true,
		Previous:     true,
		SinceSeconds: &since,
	})
	if err != nil {
		t.Fatalf("GetEvaluationLogs: %v", err)
	}
	want := fmt.Sprintf("=== pod=%s container=%s benchmark_id=bench-1 ===\nfake logs", podName, adapterContainerName)
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestGetEvaluationLogsSingleBenchmarkNoJob(t *testing.T) {
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

	idx := 0
	got, err := runtime.GetEvaluationLogs(evaluation, benchmarks, &idx, api.EvaluationLogOptions{TailLines: 10})
	if err != nil {
		t.Fatalf("GetEvaluationLogs: %v", err)
	}
	if got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

func TestGetEvaluationLogsRequiresContext(t *testing.T) {
	evaluation := sampleEvaluation("provider-1")
	runtime := &K8sRuntime{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		helper: &KubernetesHelper{clientset: fake.NewClientset()},
	}

	_, err := runtime.GetEvaluationLogs(evaluation, evaluation.Benchmarks, nil, api.EvaluationLogOptions{TailLines: 10})
	if err == nil {
		t.Fatal("expected error for nil context")
	}
}

func TestGetEvaluationLogsRejectsEmptyBenchmarks(t *testing.T) {
	evaluation := sampleEvaluation("provider-1")
	runtime := &K8sRuntime{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		helper: &KubernetesHelper{clientset: fake.NewClientset()},
		ctx:    context.Background(),
	}

	_, err := runtime.GetEvaluationLogs(evaluation, nil, nil, api.EvaluationLogOptions{TailLines: 10})
	if err == nil {
		t.Fatal("expected error for empty benchmarks")
	}
}

func TestGetEvaluationLogsRejectsNegativeBenchmarkIndex(t *testing.T) {
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

	idx := -1
	_, err = runtime.GetEvaluationLogs(evaluation, benchmarks, &idx, api.EvaluationLogOptions{TailLines: 10})
	if err == nil {
		t.Fatal("expected error for negative benchmark index")
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
