package mlflow

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/eval-hub/eval-hub/internal/config"
	"github.com/eval-hub/eval-hub/internal/messages"
	"github.com/eval-hub/eval-hub/internal/serviceerrors"
	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/eval-hub/eval-hub/pkg/mlflowclient"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

func NewMLFlowClient(config *config.Config, logger *slog.Logger) (*mlflowclient.Client, error) {
	url := ""
	if config.MLFlow != nil && config.MLFlow.TrackingURI != "" {
		url = config.MLFlow.TrackingURI
	}

	if url == "" {
		logger.Warn("MLFlow tracking URI is not set, skipping MLFlow client creation")
		return nil, nil
	}

	if config.MLFlow.HTTPTimeout == 0 {
		config.MLFlow.HTTPTimeout = 30 * time.Second
	}

	// Build TLS config if not already provided
	if config.MLFlow.TLSConfig == nil {
		tlsConfig := &tls.Config{
			MinVersion: tls.VersionTLS12,
			MaxVersion: tls.VersionTLS13,
		}

		// Load custom CA certificate if specified
		if config.MLFlow.CACertPath != "" {
			caCert, err := os.ReadFile(config.MLFlow.CACertPath)
			if err != nil {
				return nil, fmt.Errorf("failed to read MLflow CA certificate at %s: %w", config.MLFlow.CACertPath, err)
			}
			caCertPool := x509.NewCertPool()
			if !caCertPool.AppendCertsFromPEM(caCert) {
				return nil, fmt.Errorf("failed to parse MLflow CA certificate at %s: file contains no valid PEM certificates", config.MLFlow.CACertPath)
			}
			tlsConfig.RootCAs = caCertPool
			logger.Info("Loaded MLflow CA certificate", "path", config.MLFlow.CACertPath)
		}

		if config.MLFlow.InsecureSkipVerify {
			tlsConfig.InsecureSkipVerify = true
			logger.Warn("MLflow TLS certificate verification is disabled")
		}

		config.MLFlow.TLSConfig = tlsConfig
	}

	httpClient := &http.Client{
		Timeout: config.MLFlow.HTTPTimeout,
		Transport: &http.Transport{
			TLSClientConfig: config.MLFlow.TLSConfig,
		},
	}

	client := mlflowclient.NewClient(url).
		WithContext(context.Background()).
		WithLogger(logger).
		WithHTTPClient(httpClient)

	// Load auth token from file if configured, falling back to the Kubernetes SA token
	tokenPath := config.MLFlow.TokenPath
	if tokenPath == "" {
		tokenPath = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	}
	if tokenData, err := os.ReadFile(tokenPath); err == nil {
		token := strings.TrimSpace(string(tokenData))
		if token != "" {
			client = client.WithToken(token)
			logger.Info("MLflow auth token loaded", "path", tokenPath)
		}
	} else {
		logger.Warn("No MLflow auth token found", "path", tokenPath, "error", err)
	}

	// Set workspace if configured
	if config.MLFlow.Workspace != "" {
		client = client.WithWorkspace(config.MLFlow.Workspace)
		logger.Info("MLflow workspace configured", "workspace", config.MLFlow.Workspace)
	}

	if config.IsOTELEnabled() {
		currentHTTPClient := client.GetHTTPClient()
		client = client.WithHTTPClient(&http.Client{
			Transport: otelhttp.NewTransport(currentHTTPClient.Transport),
			Timeout:   currentHTTPClient.Timeout,
		})
		logger.Info("Enabled OTEL transport for MLFlow client")
	}

	logger.Info("MLFlow tracking enabled", "mlflow_experiment_url", client.GetExperimentsURL())

	return client, nil
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
