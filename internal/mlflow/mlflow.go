package mlflow

import (
	"context"
	"log/slog"

	"github.com/eval-hub/eval-hub/internal/config"
	"github.com/eval-hub/eval-hub/internal/messages"
	"github.com/eval-hub/eval-hub/internal/serviceerrors"
	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/eval-hub/eval-hub/pkg/mlflowclient"
)

func NewMLFlowClient(config *config.Config, logger *slog.Logger) *mlflowclient.Client {
	url := ""
	if config.MLFlow != nil && config.MLFlow.TrackingURI != "" {
		url = config.MLFlow.TrackingURI
	}

	if url == "" {
		logger.Warn("MLFlow tracking URI is not set, skipping MLFlow client creation")
		return nil
	}

	return mlflowclient.NewClient(url).WithContext(context.Background()).WithLogger(logger)
}

func GetExperimentID(mlflowClient *mlflowclient.Client, experiment *api.ExperimentConfig) (experimentID string, experimentURL string, err error) {
	if experiment == nil || experiment.Name == "" {
		return "", "", nil
	}

	// if we get here then we have an experiment name so we need an MLFlow client

	if mlflowClient == nil {
		return "", "", serviceerrors.NewServiceError(messages.MLFlowRequiredForExperiment)
	}

	mlflowExperiment, err := mlflowClient.GetExperimentByName(experiment.Name)
	if err != nil {
		if !mlflowclient.IsResourceDoesNotExistError(err) {
			// This is some other error than "resource does not exist" so report it as an error
			return "", "", serviceerrors.NewServiceError(messages.MLFlowRequestFailed, "Error", err.Error())
		}
	}

	if mlflowExperiment != nil && mlflowExperiment.Experiment.LifecycleStage == "active" && mlflowExperiment.Experiment.ExperimentID != "" {
		mlflowClient.GetLogger().Info("Found active experiment", "experiment_name", experiment.Name, "experiment_id", mlflowExperiment.Experiment.ExperimentID)
		// we found an active experiment with the given name so return the ID
		return mlflowExperiment.Experiment.ExperimentID, mlflowClient.GetExperimentsURL(), nil
	}

	// There is a possibility that the experiment was created between the get and the create
	// but we do not consider this worth taking into account as it is very unlikely to happen.

	// create a new experiment as we did not find an active experiment with the given name
	req := mlflowclient.CreateExperimentRequest{
		Name:             experiment.Name,
		ArtifactLocation: experiment.ArtifactLocation,
		Tags:             experiment.Tags,
	}
	resp, err := mlflowClient.CreateExperiment(&req)
	if err != nil {
		return "", "", serviceerrors.NewServiceError(messages.MLFlowRequestFailed, "Error", err.Error())
	}

	mlflowClient.GetLogger().Info("Created new experiment", "experiment_name", experiment.Name, "experiment_id", resp.ExperimentID)
	return resp.ExperimentID, mlflowClient.GetExperimentsURL(), nil
}
