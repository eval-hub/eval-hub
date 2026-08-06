package k8s

import (
	"context"
	"strings"
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/record"
)

func TestCloseWithNilBroadcaster(t *testing.T) {
	h := &KubernetesHelper{}
	if err := h.Close(); err != nil { // must not panic when broadcaster is nil
		t.Fatalf("Close: %v", err)
	}
}

func TestCloseShutdownsBroadcaster(t *testing.T) {
	h := &KubernetesHelper{broadcaster: record.NewBroadcaster()}
	if err := h.Close(); err != nil { // must not panic
		t.Fatalf("Close: %v", err)
	}
}

func TestCreateConfigMapRequiresNamespaceAndName(t *testing.T) {
	helper := &KubernetesHelper{}
	if _, err := helper.CreateConfigMap(context.Background(), "", "name", map[string]string{}, nil); err == nil {
		t.Fatalf("expected error for missing namespace")
	}
	if _, err := helper.CreateConfigMap(context.Background(), "default", "", map[string]string{}, nil); err == nil {
		t.Fatalf("expected error for missing name")
	}
}

func TestCreateJobRequiresNamespaceAndName(t *testing.T) {
	helper := &KubernetesHelper{}
	if _, err := helper.CreateJob(context.Background(), nil); err == nil {
		t.Fatalf("expected error for missing job")
	}
}

func TestDeleteConfigMapRequiresNamespaceAndName(t *testing.T) {
	helper := &KubernetesHelper{}
	if err := helper.DeleteConfigMap(context.Background(), "", "name"); err == nil {
		t.Fatalf("expected error for missing namespace")
	}
	if err := helper.DeleteConfigMap(context.Background(), "default", ""); err == nil {
		t.Fatalf("expected error for missing name")
	}
}

func TestSetConfigMapOwnerRequiresNamespaceAndName(t *testing.T) {
	helper := &KubernetesHelper{}
	if err := helper.SetConfigMapOwner(context.Background(), "", "name", emptyOwnerRef()); err == nil {
		t.Fatalf("expected error for missing namespace")
	}
	if err := helper.SetConfigMapOwner(context.Background(), "default", "", emptyOwnerRef()); err == nil {
		t.Fatalf("expected error for missing name")
	}
}

func emptyOwnerRef() metav1.OwnerReference {
	return metav1.OwnerReference{}
}

func TestSetConfigMapOwnerUpdatesOwnerReferences(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	helper := &KubernetesHelper{clientset: clientset}
	_, err := clientset.CoreV1().ConfigMaps("default").Create(context.Background(), &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "job-spec",
			Namespace: "default",
		},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("failed to create configmap: %v", err)
	}
	owner := metav1.OwnerReference{
		APIVersion: "batch/v1",
		Kind:       "Job",
		Name:       "job-1",
		UID:        "uid-1",
	}
	if err := helper.SetConfigMapOwner(context.Background(), "default", "job-spec", owner); err != nil {
		t.Fatalf("SetConfigMapOwner returned error: %v", err)
	}
	updated, err := clientset.CoreV1().ConfigMaps("default").Get(context.Background(), "job-spec", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get configmap: %v", err)
	}
	if len(updated.OwnerReferences) != 1 || updated.OwnerReferences[0].Name != "job-1" {
		t.Fatalf("expected owner reference to be set")
	}
}

func TestListPodsRequiresNamespace(t *testing.T) {
	helper := &KubernetesHelper{}
	if _, err := helper.ListPods(context.Background(), "", "job-name=foo"); err == nil {
		t.Fatal("expected error for missing namespace")
	}
}

func TestGetPodLogsRequiresNamespaceAndPodName(t *testing.T) {
	helper := &KubernetesHelper{}
	if _, err := helper.GetPodLogs(context.Background(), "", "pod-1", nil); err == nil {
		t.Fatal("expected error for missing namespace")
	}
	if _, err := helper.GetPodLogs(context.Background(), "default", "", nil); err == nil {
		t.Fatal("expected error for missing pod name")
	}
}

func TestGetPodLogsReturnsStreamContent(t *testing.T) {
	clientset := fake.NewClientset(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pod-1", Namespace: "default"},
	})
	helper := &KubernetesHelper{clientset: clientset}
	got, err := helper.GetPodLogs(context.Background(), "default", "pod-1", nil)
	if err != nil {
		t.Fatalf("GetPodLogs: %v", err)
	}
	if got != "fake logs" {
		t.Fatalf("got %q, want fake logs", got)
	}
}

func TestEmitEventRequiresRecorder(t *testing.T) {
	helper := &KubernetesHelper{}
	job := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "job-1", Namespace: "default"}}
	if err := helper.EmitEvent(job, corev1.EventTypeNormal, "Reason", "msg"); err == nil {
		t.Fatal("expected error when recorder is nil")
	}
}

func TestEmitEventRejectsUnsupportedEventType(t *testing.T) {
	fakeRecorder := record.NewFakeRecorder(10)
	helper := NewKubernetesHelperWithRecorder(fake.NewSimpleClientset(), fakeRecorder)
	job := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "job-1", Namespace: "default"}}
	if err := helper.EmitEvent(job, "Unknown", "Reason", "msg"); err == nil {
		t.Fatal("expected error for unsupported event type")
	}
}

func TestEmitEventRequiresJob(t *testing.T) {
	fakeRecorder := record.NewFakeRecorder(10)
	helper := NewKubernetesHelperWithRecorder(fake.NewSimpleClientset(), fakeRecorder)
	if err := helper.EmitEvent(nil, corev1.EventTypeNormal, "Reason", "msg"); err == nil {
		t.Fatal("expected error when job is nil")
	}
}

func TestEmitEventWritesToRecorder(t *testing.T) {
	fakeRecorder := record.NewFakeRecorder(10)
	helper := NewKubernetesHelperWithRecorder(fake.NewSimpleClientset(), fakeRecorder)
	job := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "job-1", Namespace: "default"}}
	if err := helper.EmitEvent(job, corev1.EventTypeNormal, "EvaluationStarted", "evaluation started"); err != nil {
		t.Fatalf("EmitEvent: %v", err)
	}
	select {
	case msg := <-fakeRecorder.Events:
		if !strings.Contains(msg, "EvaluationStarted") {
			t.Fatalf("expected EvaluationStarted in event, got: %s", msg)
		}
	default:
		t.Fatal("expected an event on the recorder channel")
	}
}

func TestPatchJobPhaseLabelRequiresNamespaceAndName(t *testing.T) {
	helper := &KubernetesHelper{}
	if err := helper.PatchJobPhaseLabel(context.Background(), "", "job-1", "Running"); err == nil {
		t.Fatal("expected error for missing namespace")
	}
	if err := helper.PatchJobPhaseLabel(context.Background(), "default", "", "Running"); err == nil {
		t.Fatal("expected error for missing name")
	}
}

func TestPatchJobPhaseLabelRequiresPhase(t *testing.T) {
	helper := &KubernetesHelper{}
	if err := helper.PatchJobPhaseLabel(context.Background(), "default", "job-1", ""); err == nil {
		t.Fatal("expected error for missing phase")
	}
}

func TestPatchJobPhaseLabelUpdatesLabel(t *testing.T) {
	clientset := fake.NewSimpleClientset(&batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "job-1", Namespace: "default"},
	})
	helper := NewKubernetesHelperWithClientset(clientset)
	if err := helper.PatchJobPhaseLabel(context.Background(), "default", "job-1", "Running"); err != nil {
		t.Fatalf("PatchJobPhaseLabel: %v", err)
	}
	updated, err := clientset.BatchV1().Jobs("default").Get(context.Background(), "job-1", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if got := updated.Labels["trustyai.opendatahub.io/evaluation-phase"]; got != "Running" {
		t.Fatalf("expected label value Running, got %q", got)
	}
}

func TestPatchJobStatusAnnotationRequiresNamespaceAndName(t *testing.T) {
	helper := &KubernetesHelper{}
	if err := helper.PatchJobStatusAnnotation(context.Background(), "", "job", map[string]any{}); err == nil {
		t.Fatal("expected error for missing namespace")
	}
	if err := helper.PatchJobStatusAnnotation(context.Background(), "default", "", map[string]any{}); err == nil {
		t.Fatal("expected error for missing name")
	}
}

func TestPatchJobStatusAnnotationSetsAnnotation(t *testing.T) {
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "job-1", Namespace: "default"},
	}
	clientset := fake.NewSimpleClientset(job)
	helper := &KubernetesHelper{clientset: clientset}

	payload := map[string]any{
		"phase":           "Running",
		"evaluation_id":   "eval-123",
		"benchmark_index": 0,
	}
	if err := helper.PatchJobStatusAnnotation(context.Background(), "default", "job-1", payload); err != nil {
		t.Fatalf("PatchJobStatusAnnotation: %v", err)
	}
	updated, err := clientset.BatchV1().Jobs("default").Get(context.Background(), "job-1", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	got := updated.Annotations[annotationEvaluationStatusKey]
	if got == "" {
		t.Fatal("expected evaluation-status annotation to be set")
	}
	if !strings.Contains(got, "eval-123") {
		t.Fatalf("expected evaluation_id in annotation, got: %s", got)
	}
	if !strings.Contains(got, "Running") {
		t.Fatalf("expected phase in annotation, got: %s", got)
	}
}
