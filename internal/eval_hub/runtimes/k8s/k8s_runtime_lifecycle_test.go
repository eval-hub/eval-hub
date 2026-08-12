package k8s

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/eval-hub/eval-hub/pkg/api"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/record"
)

func TestLifecycleSignalMapping(t *testing.T) {
	cases := []struct {
		state      api.State
		wantOK     bool
		wantPhase  string
		wantType   string
		wantReason string
	}{
		{api.StateRunning, true, "Running", corev1.EventTypeNormal, "EvaluationRunning"},
		{api.StateCompleted, true, "Completed", corev1.EventTypeNormal, "EvaluationCompleted"},
		{api.StateFailed, true, "Failed", corev1.EventTypeWarning, "EvaluationFailed"},
		{api.StatePending, false, "", "", ""},
		{api.StateCancelled, false, "", "", ""},
	}
	for _, tc := range cases {
		phase, eventtype, reason, ok := lifecycleSignal(tc.state)
		if ok != tc.wantOK {
			t.Errorf("lifecycleSignal(%q): ok=%v, want %v", tc.state, ok, tc.wantOK)
		}
		if !tc.wantOK {
			continue
		}
		if phase != tc.wantPhase {
			t.Errorf("lifecycleSignal(%q): phase=%q, want %q", tc.state, phase, tc.wantPhase)
		}
		if eventtype != tc.wantType {
			t.Errorf("lifecycleSignal(%q): eventtype=%q, want %q", tc.state, eventtype, tc.wantType)
		}
		if reason != tc.wantReason {
			t.Errorf("lifecycleSignal(%q): reason=%q, want %q", tc.state, reason, tc.wantReason)
		}
	}
}

func TestNotifyJobPhaseTransitionPatchesLabel(t *testing.T) {
	evaluation := sampleEvaluation("provider-1")
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "eval-job",
			Namespace: "default",
			Labels: map[string]string{
				labelJobIDKey:          sanitizeLabelValue(evaluation.Resource.ID),
				labelBenchmarkIndexKey: "0",
			},
		},
	}
	clientset := fake.NewSimpleClientset(job)
	runtime := &K8sRuntime{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		helper: &KubernetesHelper{clientset: clientset},
	}

	runtime.NotifyJobPhaseTransition(context.Background(), evaluation, 0, api.StateRunning)

	updated, err := clientset.BatchV1().Jobs("default").Get(context.Background(), "eval-job", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if got := updated.Labels[labelEvaluationPhaseKey]; got != "Running" {
		t.Fatalf("expected label value Running, got %q", got)
	}
	if got := updated.Annotations[annotationEvaluationStatusKey]; got == "" {
		t.Fatal("expected evaluation-status annotation to be set")
	}
}

func TestNotifyJobPhaseTransitionEmitsEvent(t *testing.T) {
	evaluation := sampleEvaluation("provider-1")
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "eval-job",
			Namespace: "default",
			Labels: map[string]string{
				labelJobIDKey:          sanitizeLabelValue(evaluation.Resource.ID),
				labelBenchmarkIndexKey: "0",
			},
		},
	}
	clientset := fake.NewSimpleClientset(job)
	fakeRecorder := record.NewFakeRecorder(10)
	runtime := &K8sRuntime{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		helper: NewKubernetesHelperWithRecorder(clientset, fakeRecorder),
	}

	runtime.NotifyJobPhaseTransition(context.Background(), evaluation, 0, api.StateCompleted)

	select {
	case msg := <-fakeRecorder.Events:
		if !strings.Contains(msg, "EvaluationCompleted") {
			t.Fatalf("expected EvaluationCompleted in event, got: %s", msg)
		}
	default:
		t.Fatal("expected an event on the recorder channel")
	}
}

func TestNotifyJobPhaseTransitionSkipsPendingState(t *testing.T) {
	evaluation := sampleEvaluation("provider-1")
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "eval-job",
			Namespace: "default",
			Labels: map[string]string{
				labelJobIDKey:          sanitizeLabelValue(evaluation.Resource.ID),
				labelBenchmarkIndexKey: "0",
			},
		},
	}
	clientset := fake.NewSimpleClientset(job)
	fakeRecorder := record.NewFakeRecorder(10)
	runtime := &K8sRuntime{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		helper: NewKubernetesHelperWithRecorder(clientset, fakeRecorder),
	}

	runtime.NotifyJobPhaseTransition(context.Background(), evaluation, 0, api.StatePending)

	select {
	case msg := <-fakeRecorder.Events:
		t.Fatalf("expected no event for Pending state, got: %s", msg)
	default:
	}
}

func TestNotifyJobPhaseTransitionNoJobFound(t *testing.T) {
	evaluation := sampleEvaluation("provider-1")
	clientset := fake.NewSimpleClientset()
	runtime := &K8sRuntime{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		helper: &KubernetesHelper{clientset: clientset},
	}
	// No matching job — should be a no-op with no panic.
	runtime.NotifyJobPhaseTransition(context.Background(), evaluation, 0, api.StateRunning)
}

func TestNotifyJobPhaseTransitionListJobsError(t *testing.T) {
	evaluation := sampleEvaluation("provider-1")
	clientset := fake.NewSimpleClientset()
	var listCalled int
	clientset.PrependReactor("list", "jobs", func(_ ktesting.Action) (bool, kruntime.Object, error) {
		listCalled++
		return true, nil, fmt.Errorf("api unavailable")
	})
	rt := &K8sRuntime{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		helper: &KubernetesHelper{clientset: clientset},
	}
	rt.NotifyJobPhaseTransition(context.Background(), evaluation, 0, api.StateRunning)
	if listCalled != 1 {
		t.Fatalf("expected list jobs to be called once, got %d", listCalled)
	}
}

func TestNotifyJobPhaseTransitionPatchLabelError(t *testing.T) {
	evaluation := sampleEvaluation("provider-1")
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "eval-job",
			Namespace: "default",
			Labels: map[string]string{
				labelJobIDKey:          sanitizeLabelValue(evaluation.Resource.ID),
				labelBenchmarkIndexKey: "0",
			},
		},
	}
	clientset := fake.NewSimpleClientset(job)
	var patchCalled int
	clientset.PrependReactor("patch", "jobs", func(_ ktesting.Action) (bool, kruntime.Object, error) {
		patchCalled++
		return true, nil, fmt.Errorf("patch forbidden")
	})
	fakeRecorder := record.NewFakeRecorder(10)
	rt := &K8sRuntime{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		helper: NewKubernetesHelperWithRecorder(clientset, fakeRecorder),
	}
	rt.NotifyJobPhaseTransition(context.Background(), evaluation, 0, api.StateRunning)
	if patchCalled != 2 {
		t.Fatalf("expected patch to be called twice (phase label + status annotation), got %d", patchCalled)
	}
	select {
	case msg := <-fakeRecorder.Events:
		if !strings.Contains(msg, "EvaluationRunning") {
			t.Fatalf("expected EvaluationRunning in event, got: %s", msg)
		}
	default:
		t.Fatal("expected an event to be emitted even when patch fails")
	}
}

func TestNotifyJobPhaseTransitionFailedState(t *testing.T) {
	evaluation := sampleEvaluation("provider-1")
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "eval-job",
			Namespace: "default",
			Labels: map[string]string{
				labelJobIDKey:          sanitizeLabelValue(evaluation.Resource.ID),
				labelBenchmarkIndexKey: "0",
			},
		},
	}
	clientset := fake.NewSimpleClientset(job)
	fakeRecorder := record.NewFakeRecorder(10)
	runtime := &K8sRuntime{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		helper: NewKubernetesHelperWithRecorder(clientset, fakeRecorder),
	}

	runtime.NotifyJobPhaseTransition(context.Background(), evaluation, 0, api.StateFailed)

	updated, err := clientset.BatchV1().Jobs("default").Get(context.Background(), "eval-job", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if got := updated.Labels[labelEvaluationPhaseKey]; got != "Failed" {
		t.Fatalf("expected label value Failed, got %q", got)
	}
	if got := updated.Annotations[annotationEvaluationStatusKey]; got == "" {
		t.Fatal("expected evaluation-status annotation to be set for Failed state")
	}

	select {
	case msg := <-fakeRecorder.Events:
		if !strings.Contains(msg, "EvaluationFailed") {
			t.Fatalf("expected EvaluationFailed in event, got: %s", msg)
		}
	default:
		t.Fatal("expected a Warning event for Failed state")
	}
}

func TestNotifyThresholdViolationEmitsEnrichedEvent(t *testing.T) {
	evaluation := sampleEvaluation("provider-1")
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "eval-job",
			Namespace: "default",
			Labels: map[string]string{
				labelJobIDKey:          sanitizeLabelValue(evaluation.Resource.ID),
				labelBenchmarkIndexKey: "0",
			},
		},
	}
	clientset := fake.NewSimpleClientset(job)
	fakeRecorder := record.NewFakeRecorder(10)
	runtime := &K8sRuntime{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		helper: NewKubernetesHelperWithRecorder(clientset, fakeRecorder),
	}

	runtime.NotifyThresholdViolation(context.Background(), evaluation, 0, "accuracy", 0.4231, 0.7000)

	select {
	case msg := <-fakeRecorder.Events:
		if !strings.Contains(msg, "EvaluationThresholdViolated") {
			t.Fatalf("expected EvaluationThresholdViolated in event, got: %s", msg)
		}
		if !strings.Contains(msg, "accuracy") {
			t.Fatalf("expected metric name 'accuracy' in event message, got: %s", msg)
		}
		if !strings.Contains(msg, "0.4231") {
			t.Fatalf("expected actual value '0.4231' in event message, got: %s", msg)
		}
		if !strings.Contains(msg, "0.7000") {
			t.Fatalf("expected threshold '0.7000' in event message, got: %s", msg)
		}
	default:
		t.Fatal("expected an EvaluationThresholdViolated event on the recorder channel")
	}
}

func TestNotifyThresholdViolationPatchesPhaseLabel(t *testing.T) {
	evaluation := sampleEvaluation("provider-1")
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "eval-job",
			Namespace: "default",
			Labels: map[string]string{
				labelJobIDKey:          sanitizeLabelValue(evaluation.Resource.ID),
				labelBenchmarkIndexKey: "0",
			},
		},
	}
	clientset := fake.NewSimpleClientset(job)
	runtime := &K8sRuntime{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		helper: &KubernetesHelper{clientset: clientset},
	}

	runtime.NotifyThresholdViolation(context.Background(), evaluation, 0, "f1_score", 0.3, 0.6)

	updated, err := clientset.BatchV1().Jobs("default").Get(context.Background(), "eval-job", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if got := updated.Labels[labelEvaluationPhaseKey]; got != EvaluationPhaseThresholdViolated {
		t.Fatalf("expected label value %q, got %q", EvaluationPhaseThresholdViolated, got)
	}
}

func TestNotifyThresholdViolationNoJobFound(t *testing.T) {
	evaluation := sampleEvaluation("provider-1")
	clientset := fake.NewSimpleClientset()
	runtime := &K8sRuntime{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		helper: &KubernetesHelper{clientset: clientset},
	}
	// No matching job — should be a no-op with no panic.
	runtime.NotifyThresholdViolation(context.Background(), evaluation, 0, "accuracy", 0.4, 0.8)
}

func TestNotifyThresholdViolationListJobsError(t *testing.T) {
	evaluation := sampleEvaluation("provider-1")
	clientset := fake.NewSimpleClientset()
	var listCalled int
	clientset.PrependReactor("list", "jobs", func(_ ktesting.Action) (bool, kruntime.Object, error) {
		listCalled++
		return true, nil, fmt.Errorf("api unavailable")
	})
	rt := &K8sRuntime{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		helper: &KubernetesHelper{clientset: clientset},
	}
	// Should not panic; error is absorbed internally.
	rt.NotifyThresholdViolation(context.Background(), evaluation, 0, "accuracy", 0.4, 0.8)
	if listCalled != 1 {
		t.Fatalf("expected list jobs to be called once, got %d", listCalled)
	}
}

func TestNotifyThresholdViolationPatchLabelError(t *testing.T) {
	evaluation := sampleEvaluation("provider-1")
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "eval-job",
			Namespace: "default",
			Labels: map[string]string{
				labelJobIDKey:          sanitizeLabelValue(evaluation.Resource.ID),
				labelBenchmarkIndexKey: "0",
			},
		},
	}
	clientset := fake.NewSimpleClientset(job)
	var patchCalled int
	clientset.PrependReactor("patch", "jobs", func(_ ktesting.Action) (bool, kruntime.Object, error) {
		patchCalled++
		return true, nil, fmt.Errorf("patch forbidden")
	})
	fakeRecorder := record.NewFakeRecorder(10)
	rt := &K8sRuntime{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		helper: NewKubernetesHelperWithRecorder(clientset, fakeRecorder),
	}
	rt.NotifyThresholdViolation(context.Background(), evaluation, 0, "accuracy", 0.4, 0.8)
	if patchCalled != 1 {
		t.Fatalf("expected patch to be called once, got %d", patchCalled)
	}
	// Event should still be emitted even when the phase-label patch fails.
	select {
	case msg := <-fakeRecorder.Events:
		if !strings.Contains(msg, "EvaluationThresholdViolated") {
			t.Fatalf("expected EvaluationThresholdViolated in event, got: %s", msg)
		}
	default:
		t.Fatal("expected an event to be emitted even when patch fails")
	}
}

func TestNotifyThresholdViolationEmitsWarningEventType(t *testing.T) {
	evaluation := sampleEvaluation("provider-1")
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "eval-job",
			Namespace: "default",
			Labels: map[string]string{
				labelJobIDKey:          sanitizeLabelValue(evaluation.Resource.ID),
				labelBenchmarkIndexKey: "2",
			},
		},
	}
	clientset := fake.NewSimpleClientset(job)
	fakeRecorder := record.NewFakeRecorder(10)
	runtime := &K8sRuntime{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		helper: NewKubernetesHelperWithRecorder(clientset, fakeRecorder),
	}

	runtime.NotifyThresholdViolation(context.Background(), evaluation, 2, "bleu", 0.1, 0.5)

	select {
	case msg := <-fakeRecorder.Events:
		if !strings.Contains(msg, corev1.EventTypeWarning) {
			t.Fatalf("expected Warning event type, got: %s", msg)
		}
	default:
		t.Fatal("expected an event on the recorder channel")
	}
}
