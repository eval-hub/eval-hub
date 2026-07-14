package ociclient

import "testing"

func TestEvaluationCardManifestTag(t *testing.T) {
	if got := EvaluationCardManifestTag("job-1", ""); got != "evaluation-card-job-1" {
		t.Fatalf("got %q", got)
	}
	if got := EvaluationCardManifestTag("job-1", "eval-123"); got != "eval-123-job-1" {
		t.Fatalf("got %q", got)
	}
}

func TestEvaluationCardLayerTitle(t *testing.T) {
	if got := EvaluationCardLayerTitle("job-1"); got != "evaluation-card-job-1.json" {
		t.Fatalf("got %q", got)
	}
}

func TestArtifactConfigBlobForJob(t *testing.T) {
	got, err := artifactConfigBlobForJob("job-1")
	if err != nil {
		t.Fatalf("artifactConfigBlobForJob() err = %v", err)
	}
	if string(got) != `{"evaluation_job_id":"job-1"}` {
		t.Fatalf("got %s", got)
	}
}

func TestValidateEvaluationJobID(t *testing.T) {
	if err := validateEvaluationJobID(""); err == nil {
		t.Fatal("expected error for empty job id")
	}
	if err := validateEvaluationJobID("job-1"); err != nil {
		t.Fatalf("validateEvaluationJobID() err = %v", err)
	}
}

func TestEvaluationCardManifestTagEmptyJobID(t *testing.T) {
	if got := EvaluationCardManifestTag("", "custom-tag"); got != "custom-tag" {
		t.Fatalf("got %q", got)
	}
}
