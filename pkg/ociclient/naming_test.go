package ociclient

import (
	"strings"
	"testing"
)

func TestEvaluationBundleManifestTag(t *testing.T) {
	uuid := "01234567-89ab-cdef-0123-456789abcdef"
	tests := []struct {
		name, jobID, prefix, want string
		wantErr                   bool
	}{
		{name: "default", jobID: uuid, want: uuid},
		{name: "custom", jobID: uuid, prefix: "final", want: "final-" + uuid},
		{name: "allowed characters", jobID: uuid, prefix: "_AZaz09.-_", want: "_AZaz09.-_-" + uuid},
		{name: "UUID boundary", jobID: uuid, prefix: strings.Repeat("a", 91), want: strings.Repeat("a", 91) + "-" + uuid},
		{name: "UUID over boundary", jobID: uuid, prefix: strings.Repeat("a", 92), want: strings.Repeat("a", 91) + "-" + uuid},
		{name: "short job ID boundary", jobID: "job-1", prefix: strings.Repeat("a", 122), want: strings.Repeat("a", 122) + "-job-1"},
		{name: "short job ID over boundary", jobID: "job-1", prefix: strings.Repeat("a", 123), want: strings.Repeat("a", 122) + "-job-1"},
		{name: "long job ID boundary", jobID: strings.Repeat("j", 127), prefix: "a", wantErr: true},
		{name: "default boundary", jobID: strings.Repeat("j", 128), want: strings.Repeat("j", 128)},
		{name: "default over boundary", jobID: strings.Repeat("j", 129), wantErr: true},
		{name: "long prefix", jobID: uuid, prefix: strings.Repeat("a", 200), want: strings.Repeat("a", 91) + "-" + uuid},
		{name: "invalid beyond truncation", jobID: uuid, prefix: strings.Repeat("a", 200) + "/", wantErr: true},
		{name: "empty job ID", prefix: "final", wantErr: true},
		{name: "job ID whitespace", jobID: " job-1 ", prefix: "final", wantErr: true},
		{name: "job ID syntax", jobID: "job/1", prefix: "final", wantErr: true},
	}
	for _, prefix := range []string{" ", " final", "final ", "fi nal", "final\n", "final\t", "a/b", "a:b", "éval", ".final", "-final"} {
		tests = append(tests, struct {
			name, jobID, prefix, want string
			wantErr                   bool
		}{name: "invalid prefix " + prefix, jobID: uuid, prefix: prefix, wantErr: true})
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := EvaluationBundleManifestTag(tt.jobID, tt.prefix)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %t", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("tag = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEvaluationCardManifestTag(t *testing.T) {
	if got := EvaluationCardManifestTag("job-1"); got != "evaluation-card-job-1" {
		t.Fatalf("got %q", got)
	}
	if got := EvaluationCardManifestTag("  job-1  "); got != "evaluation-card-job-1" {
		t.Fatalf("got %q", got)
	}
}

func TestEvaluationCardLayerTitle(t *testing.T) {
	if got := EvaluationCardLayerTitle("job-1"); got != "evaluation-card-job-1.json" {
		t.Fatalf("got %q", got)
	}
	if got := EvaluationCardLayerTitle(""); got != "evaluation-card.json" {
		t.Fatalf("got %q", got)
	}
}

func TestEvaluationCardConfigTitle(t *testing.T) {
	if got := EvaluationCardConfigTitle("job-1"); got != "evaluation-card-job-1-config.json" {
		t.Fatalf("got %q", got)
	}
	if got := EvaluationCardConfigTitle(""); got != "evaluation-card-config.json" {
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
	got, err = artifactConfigBlobForJob("")
	if err != nil || string(got) != "{}" {
		t.Fatalf("artifactConfigBlobForJob(\"\") = %q err=%v", got, err)
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
	if got := EvaluationCardManifestTag(""); got != "" {
		t.Fatalf("got %q", got)
	}
}
