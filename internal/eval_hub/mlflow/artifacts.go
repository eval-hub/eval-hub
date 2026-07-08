package mlflow

import (
	"fmt"
	"strings"

	"github.com/eval-hub/eval-hub/pkg/mlflowclient"
)

const (
	EvalCardArtifactFileName = "evaluation-card.json"
	EvalCardRunTagKey        = "evaluation_job_id"
)

// BuildRunArtifactPath returns the MLflow proxied artifact path for a run-relative file.
func BuildRunArtifactPath(experimentID, runID, relativeArtifactPath string) string {
	relativeArtifactPath = strings.TrimPrefix(strings.TrimSpace(relativeArtifactPath), "/")
	return fmt.Sprintf("%s/%s/artifacts/%s", experimentID, runID, relativeArtifactPath)
}

// UploadArtifactToExperiment uploads content to a run artifact path under an experiment.
func UploadArtifactToExperiment(
	client *mlflowclient.Client,
	experimentID string,
	runID string,
	relativeArtifactPath string,
	content []byte,
	contentType string,
) (string, error) {
	if client == nil {
		return "", fmt.Errorf("mlflow client is nil")
	}
	if strings.TrimSpace(experimentID) == "" {
		return "", fmt.Errorf("experiment id is required")
	}
	if strings.TrimSpace(runID) == "" {
		return "", fmt.Errorf("run id is required")
	}
	if err := client.EnsureWorkspace(); err != nil {
		return "", err
	}
	return client.UploadArtifact(
		BuildRunArtifactPath(experimentID, runID, relativeArtifactPath),
		content,
		contentType,
	)
}

// CreateEvaluationCardRun creates a new MLflow run for storing evaluation card artifacts.
func CreateEvaluationCardRun(client *mlflowclient.Client, experimentID, jobID, runName string) (string, error) {
	if client == nil {
		return "", fmt.Errorf("mlflow client is nil")
	}
	if strings.TrimSpace(experimentID) == "" {
		return "", fmt.Errorf("experiment id is required")
	}
	if strings.TrimSpace(jobID) == "" {
		return "", fmt.Errorf("job id is required")
	}
	if err := client.EnsureWorkspace(); err != nil {
		return "", err
	}

	if strings.TrimSpace(runName) == "" {
		runName = "evaluation-card-" + jobID
	}
	createResp, err := client.CreateRun(&mlflowclient.CreateRunRequest{
		ExperimentID: experimentID,
		RunName:      runName,
		Tags: []mlflowclient.RunTag{
			{Key: EvalCardRunTagKey, Value: jobID},
			{Key: "context", Value: "eval-hub"},
		},
	})
	if err != nil {
		return "", err
	}
	if createResp == nil || createResp.Run.Info.RunID == "" {
		return "", fmt.Errorf("mlflow create run response missing run id")
	}
	return createResp.Run.Info.RunID, nil
}

// PersistEvalCard uploads the evaluation card JSON artifact to a new MLflow run in the job's experiment.
func PersistEvalCard(client *mlflowclient.Client, experimentID, jobID, runName string, cardJSON []byte) (string, error) {
	runID, err := CreateEvaluationCardRun(client, experimentID, jobID, runName)
	if err != nil {
		return "", err
	}
	artifactPath := BuildRunArtifactPath(experimentID, runID, EvalCardArtifactFileName)
	return client.UploadArtifact(artifactPath, cardJSON, "application/json")
}
