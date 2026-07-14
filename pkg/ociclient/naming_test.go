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
