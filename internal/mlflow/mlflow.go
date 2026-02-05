package mlflow

import (
	"github.com/eval-hub/eval-hub/internal/executioncontext"
	"github.com/eval-hub/eval-hub/internal/messages"
	"github.com/eval-hub/eval-hub/internal/serviceerrors"
	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/eval-hub/eval-hub/pkg/mlflowclient"
)

func GetExperimentID(ctx *executioncontext.ExecutionContext, mlflowClient *mlflowclient.Client, experiment *api.ExperimentConfig) (string, error) {
	if experiment == nil {
		return "", nil
	}

	if mlflowClient == nil {
		return "", serviceerrors.NewServiceError(messages.MLFlowRequiredForExperiment)
	}

	mlflowExperiment, err := mlflowClient.GetExperimentByName(experiment.Name)
	if err != nil {
		return "", serviceerrors.NewServiceError(messages.MLFlowRequestFailed, "Error", err.Error())
	}

	if mlflowExperiment != nil && mlflowExperiment.Experiment.LifecycleStage == "active" && mlflowExperiment.Experiment.ExperimentID != "" {
		return mlflowExperiment.Experiment.ExperimentID, nil
	}

	// create a new experiment as we did not find an active experiment with the given name
	req := mlflowclient.CreateExperimentRequest{
		Name:             experiment.Name,
		ArtifactLocation: experiment.ArtifactLocation,
		Tags:             experiment.Tags,
	}
	resp, err := mlflowClient.CreateExperiment(&req)
	if err != nil {
		return "", serviceerrors.NewServiceError(messages.MLFlowRequestFailed, "Error", err.Error())
	}
	return resp.ExperimentID, nil
}
