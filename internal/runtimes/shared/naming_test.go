package shared

import (
	"strings"
	"testing"
)

func TestBuildK8sNameSanitizes(t *testing.T) {
	name := BuildK8sName("Job-123", "Provider-1", "AraDiCE_boolq_lev", "")
	prefix := "eval-job-provider-1-aradice-boolq-lev-job-123-"
	if !strings.HasPrefix(name, prefix) {
		t.Fatalf("expected sanitized name to start with %q, got %q", prefix, name)
	}
}

func TestBuildK8sNameDiffersAcrossProviders(t *testing.T) {
	jobID := "job-123"
	benchmarkID := "arc_easy"
	name1 := BuildK8sName(jobID, "lmeval", benchmarkID, "")
	name2 := BuildK8sName(jobID, "lighteval", benchmarkID, "")
	if name1 == name2 {
		t.Fatalf("expected different names for different providers, got %q", name1)
	}
}
