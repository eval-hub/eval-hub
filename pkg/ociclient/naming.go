package ociclient

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	// AnnotationEvaluationJobID is the OCI annotation key for the evaluation job identifier.
	AnnotationEvaluationJobID = "evaluation_job_id"
	// AnnotationImageTitle is the standard OCI title annotation used as a human-readable blob/manifest name.
	AnnotationImageTitle = "org.opencontainers.image.title"

	evaluationCardArtifactPrefix = "evaluation-card"
)

// EvaluationCardManifestTag returns the OCI manifest tag for an evaluation card artifact.
// The evaluation job id is always included so each job maps to a distinct tagged manifest.
func EvaluationCardManifestTag(jobID, ociTag string) string {
	jobID = strings.TrimSpace(jobID)
	ociTag = strings.TrimSpace(ociTag)
	if jobID == "" {
		return ociTag
	}
	if ociTag == "" {
		return evaluationCardArtifactPrefix + "-" + jobID
	}
	return ociTag + "-" + jobID
}

// EvaluationCardLayerTitle returns the OCI layer title for the evaluation card JSON blob.
func EvaluationCardLayerTitle(jobID string) string {
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return evaluationCardArtifactPrefix + ".json"
	}
	return evaluationCardArtifactPrefix + "-" + jobID + ".json"
}

// EvaluationCardConfigTitle returns the OCI config descriptor title for the artifact config blob.
func EvaluationCardConfigTitle(jobID string) string {
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return evaluationCardArtifactPrefix + "-config.json"
	}
	return evaluationCardArtifactPrefix + "-" + jobID + "-config.json"
}

// artifactConfigBlobForJob returns a minimal per-job OCI artifact config payload.
func artifactConfigBlobForJob(jobID string) ([]byte, error) {
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return []byte("{}"), nil
	}
	return json.Marshal(map[string]string{AnnotationEvaluationJobID: jobID})
}

// mergeEvaluationCardAnnotations combines user annotations with eval-hub defaults for an artifact.
func mergeEvaluationCardAnnotations(jobID, manifestTag string, user map[string]string) map[string]string {
	out := make(map[string]string, len(user)+2)
	for key, value := range user {
		out[key] = value
	}
	if jobID != "" {
		out[AnnotationEvaluationJobID] = jobID
	}
	if manifestTag != "" {
		out[AnnotationImageTitle] = manifestTag
	}
	return out
}

// layerAnnotations returns OCI layer descriptor annotations for an evaluation card blob.
func layerAnnotations(jobID string) map[string]string {
	title := EvaluationCardLayerTitle(jobID)
	annotations := map[string]string{
		AnnotationImageTitle: title,
	}
	if jobID != "" {
		annotations[AnnotationEvaluationJobID] = jobID
	}
	return annotations
}

// configAnnotations returns OCI config descriptor annotations for the artifact config blob.
func configAnnotations(jobID string) map[string]string {
	title := EvaluationCardConfigTitle(jobID)
	annotations := map[string]string{
		AnnotationImageTitle: title,
	}
	if jobID != "" {
		annotations[AnnotationEvaluationJobID] = jobID
	}
	return annotations
}

// validateEvaluationJobID ensures the job id is present for artifact naming.
func validateEvaluationJobID(jobID string) error {
	if strings.TrimSpace(jobID) == "" {
		return fmt.Errorf("evaluation job id is required")
	}
	return nil
}
