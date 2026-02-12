package mlflow

import (
	"context"
	"crypto/tls"
	"log/slog"
	"net/http"
	"time"

	"github.com/eval-hub/eval-hub/internal/config"
	"github.com/eval-hub/eval-hub/internal/messages"
	"github.com/eval-hub/eval-hub/internal/serviceerrors"
	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/eval-hub/eval-hub/pkg/mlflowclient"
)

func NewMLFlowClient(mlflowConfig *config.MLFlowConfig, logger *slog.Logger) *mlflowclient.Client {
	url := ""
	if mlflowConfig != nil && mlflowConfig.TrackingURI != "" {
		url = mlflowConfig.TrackingURI
	}

	if url == "" {
		logger.Warn("MLFlow tracking URI is not set, skipping MLFlow client creation")
		return nil
	}

	if mlflowConfig.HTTPTimeout == 0 {
		mlflowConfig.HTTPTimeout = 30 * time.Second
	}

	// for now we create a default TLS config if the secure flag is set and no TLS config is provided
	if mlflowConfig.Secure && (mlflowConfig.TLSConfig == nil) {
		mlflowConfig.TLSConfig = &tls.Config{
			InsecureSkipVerify: false,
			RootCAs:            nil, // we might need to load certificates
			ClientCAs:          nil, // we might need to load certificates
			ClientAuth:         tls.NoClientCert,
			MinVersion:         tls.VersionTLS12,
			MaxVersion:         tls.VersionTLS13,
		}
	}

	httpClient := &http.Client{
		Timeout: mlflowConfig.HTTPTimeout,
	}

	if mlflowConfig.TLSConfig != nil {
		httpClient = &http.Client{
			Timeout: mlflowConfig.HTTPTimeout,
			Transport: &http.Transport{
				TLSClientConfig: mlflowConfig.TLSConfig,
			},
		}
	}

	client := mlflowclient.NewClient(url).
		WithContext(context.Background()). // this is a fallback, each request needs to provide the context for the API call
		WithLogger(logger).
		WithHTTPClient(httpClient)

	logger.Info("MLFlow tracking enabled", "mlflow_experiment_url", client.GetExperimentsURL())

	return client
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
