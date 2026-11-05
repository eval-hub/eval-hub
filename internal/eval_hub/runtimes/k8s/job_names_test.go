package k8s

import (
	"strings"
	"testing"
)

func TestBuildK8sNameSanitizes(t *testing.T) {
	name := buildK8sName("Job-123", "Guid-ABC", "")
	if !strings.HasPrefix(name, "job-123-") {
		t.Fatalf("expected sanitized name to start with %q, got %q", "job-123-", name)
	}
}

func TestBuildK8sNameDiffersAcrossGUIDs(t *testing.T) {
	jobID := "job-123"
	name1 := buildK8sName(jobID, "guid-1", "")
	name2 := buildK8sName(jobID, "guid-2", "")
	if name1 == name2 {
		t.Fatalf("expected different names for different GUIDs, got %q", name1)
	}
}

func TestBuildK8sNameRespectsMaxLengthWithSpecSuffix(t *testing.T) {
	longJobID := strings.Repeat("a", 80)
	longGUID := strings.Repeat("b", 40)
	name := buildK8sName(longJobID, longGUID, specSuffix)
	if len(name) > maxK8sNameLength {
		t.Fatalf("expected name length <= %d, got %d (%q)", maxK8sNameLength, len(name), name)
	}
	if !strings.HasSuffix(name, specSuffix) {
		t.Fatalf("expected configMap suffix %q on truncated name, got %q", specSuffix, name)
	}
}

func TestJobAndConfigMapShareTruncatedBase(t *testing.T) {
	// UUID jobID + UUID execution GUID always exceed the 63-char DNS-1123 limit.
	jobID := "58755fb1-05d2-4410-87c6-2c47100258c8"
	guid := "8cdd1d84-3297-4aca-ad25-a4ea9e382723"

	job := jobName(jobID, guid)
	cm := configMapName(jobID, guid)

	if len(job) > maxK8sNameLength {
		t.Fatalf("job name length <= %d, got %d (%q)", maxK8sNameLength, len(job), job)
	}
	if len(cm) > maxK8sNameLength {
		t.Fatalf("configmap name length <= %d, got %d (%q)", maxK8sNameLength, len(cm), cm)
	}
	if !strings.HasSuffix(cm, specSuffix) {
		t.Fatalf("expected configmap suffix %q, got %q", specSuffix, cm)
	}
	if got, want := cm, job+specSuffix; got != want {
		t.Fatalf("configmap must be job name + %q;\n job=%q\n  cm=%q\nwant=%q", specSuffix, job, got, want)
	}
}

func TestJobAndConfigMapShareBaseWithLongIDs(t *testing.T) {
	jobID := strings.Repeat("a", 80)
	guid := strings.Repeat("b", 40)
	job := jobName(jobID, guid)
	cm := configMapName(jobID, guid)
	if got, want := cm, job+specSuffix; got != want {
		t.Fatalf("configmap must be job name + %q;\n job=%q (%d)\n  cm=%q (%d)", specSuffix, job, len(job), cm, len(cm))
	}
	if len(cm) > maxK8sNameLength {
		t.Fatalf("configmap name length <= %d, got %d", maxK8sNameLength, len(cm))
	}
}

func TestJobLabelsNilConfig(t *testing.T) {
	labels := jobLabels(nil)
	if len(labels) != 0 {
		t.Fatalf("expected empty labels for nil cfg, got %v", labels)
	}
}

func TestJobLabelsSanitizeBenchmarkID(t *testing.T) {
	labels := jobLabels(&jobConfig{jobID: "job-123", providerID: "lighteval", benchmarkID: "arc:easy", benchmarkIndex: 0})
	if labels[labelBenchmarkIDKey] != "arc-easy" {
		t.Fatalf("expected benchmark label to be sanitized, got %q", labels[labelBenchmarkIDKey])
	}
	if labels[labelBenchmarkIndexKey] != "0" {
		t.Fatalf("expected benchmark_index label %q, got %q", "0", labels[labelBenchmarkIndexKey])
	}
}

func TestJobLabelsEvalHubInstance(t *testing.T) {
	labels := jobLabels(&jobConfig{jobID: "j", providerID: "p", benchmarkID: "b", benchmarkIndex: 0, evalHubInstanceName: "my-evalhub", evalHubCRNamespace: "prod-ns"})
	if labels[labelEvalHubInstanceNameKey] != "my-evalhub" {
		t.Fatalf("instance-name: got %q", labels[labelEvalHubInstanceNameKey])
	}
	if labels[labelEvalHubInstanceNamespaceKey] != "prod-ns" {
		t.Fatalf("instance-namespace: got %q", labels[labelEvalHubInstanceNamespaceKey])
	}
	empty := jobLabels(&jobConfig{jobID: "j", providerID: "p", benchmarkID: "b", benchmarkIndex: 0})
	if _, ok := empty[labelEvalHubInstanceNameKey]; ok {
		t.Fatal("expected no instance labels when name/namespace empty")
	}
}

func TestJobLabelsKueueQueueName(t *testing.T) {
	labels := jobLabels(&jobConfig{jobID: "j", providerID: "p", benchmarkID: "b", benchmarkIndex: 0, queueKind: "kueue", queueName: "my-queue", priorityClassName: "high-priority"})
	if labels[labelKueueQueueNameKey] != "my-queue" {
		t.Fatalf("expected kueue queue label %q, got %q", "my-queue", labels[labelKueueQueueNameKey])
	}
	if labels[labelKueuePriorityClassKey] != "high-priority" {
		t.Fatalf("expected kueue priority label %q, got %q", "high-priority", labels[labelKueuePriorityClassKey])
	}
	noQueue := jobLabels(&jobConfig{jobID: "j", providerID: "p", benchmarkID: "b", benchmarkIndex: 0})
	if _, ok := noQueue[labelKueueQueueNameKey]; ok {
		t.Fatal("expected no kueue queue label when queue name is empty")
	}
	nonKueue := jobLabels(&jobConfig{jobID: "j", providerID: "p", benchmarkID: "b", benchmarkIndex: 0, queueKind: "other", queueName: "my-queue"})
	if _, ok := nonKueue[labelKueueQueueNameKey]; ok {
		t.Fatal("expected no kueue queue label when queue kind is not kueue")
	}
}

func TestJobLabelsIncludesEvaluationPhasePending(t *testing.T) {
	labels := jobLabels(&jobConfig{jobID: "j", providerID: "p", benchmarkID: "b", benchmarkIndex: 0})
	if got := labels[labelEvaluationPhaseKey]; got != EvaluationPhasePending {
		t.Fatalf("expected evaluation-phase label %q, got %q", EvaluationPhasePending, got)
	}
}
