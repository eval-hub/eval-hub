package ociclient

import (
	"strings"
	"testing"
)

func FuzzEvaluationCardManifestTag(f *testing.F) {
	f.Add("job-123", "v1.0")
	f.Add("job-123", "")
	f.Add("", "v1.0")
	f.Add("", "")
	f.Add("  job-123  ", "  v1.0  ")

	f.Fuzz(func(t *testing.T, jobID, ociTag string) {
		tag := EvaluationCardManifestTag(jobID, ociTag)

		trimJob := strings.TrimSpace(jobID)
		trimTag := strings.TrimSpace(ociTag)

		if trimJob == "" && trimTag == "" && tag != "" {
			t.Fatalf("both empty inputs should produce empty tag, got %q", tag)
		}
		if trimJob != "" && !strings.Contains(tag, trimJob) {
			t.Fatalf("tag %q should contain jobID %q", tag, trimJob)
		}
	})
}

func FuzzEvaluationCardLayerTitle(f *testing.F) {
	f.Add("job-123")
	f.Add("")
	f.Add("  spaces  ")

	f.Fuzz(func(t *testing.T, jobID string) {
		title := EvaluationCardLayerTitle(jobID)
		if !strings.HasSuffix(title, ".json") {
			t.Fatalf("layer title %q does not end with .json", title)
		}
		if !strings.HasPrefix(title, evaluationCardArtifactPrefix) {
			t.Fatalf("layer title %q does not start with expected prefix", title)
		}
	})
}

func FuzzValidateEvaluationJobID(f *testing.F) {
	f.Add("job-123")
	f.Add("")
	f.Add("   ")
	f.Add("a")

	f.Fuzz(func(t *testing.T, jobID string) {
		err := validateEvaluationJobID(jobID)
		if strings.TrimSpace(jobID) == "" {
			if err == nil {
				t.Fatal("expected error for empty/whitespace-only jobID")
			}
		} else {
			if err != nil {
				t.Fatalf("unexpected error for non-empty jobID %q: %v", jobID, err)
			}
		}
	})
}
