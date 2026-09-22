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

	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
	"github.com/eval-hub/eval-hub/internal/eval_hub/messages"
	"github.com/eval-hub/eval-hub/internal/eval_hub/serviceerrors"
	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/eval-hub/eval-hub/pkg/mlflowclient"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

func SetupMLFlowClient(config *config.Config, logger *slog.Logger) (*mlflowclient.Client, *WorkspaceSupport, string, string, error) {
	mlflowClient, workspaceSupport, err := NewMLFlowClient(config, logger)
	if err != nil {
		return nil, nil, "", "", err
	}
	if mlflowClient == nil {
		// this is the case when no tracking URI is set
		return nil, nil, "", "", nil
	}
	serverVersion, err := mlflowClient.GetVersion()
	if err != nil {
		// for now a failure to get the mlflow server version will not stop the server startup
		// because maybe the port is not yet ready or the server is not yet running
		logger.Warn("Failed to get MLFlow server version", "error", err.Error())
	}
	// if we get here then we have a valid tracking URI
	return mlflowClient, workspaceSupport, config.MLFlow.TrackingURI, serverVersion, nil
}

func NewMLFlowClient(config *config.Config, logger *slog.Logger) (*mlflowclient.Client, *WorkspaceSupport, error) {
	url := ""
	if config.MLFlow != nil && config.MLFlow.TrackingURI != "" {
		url = config.MLFlow.TrackingURI
	}

	if url == "" {
		logger.Warn("MLFlow tracking URI is not set, skipping MLFlow client creation")
		return nil, nil, nil
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
				return nil, nil, fmt.Errorf("failed to read MLflow CA certificate at %s: %w", config.MLFlow.CACertPath, err)
			}
			caCertPool := x509.NewCertPool()
			if !caCertPool.AppendCertsFromPEM(caCert) {
				return nil, nil, fmt.Errorf("failed to parse MLflow CA certificate at %s: file contains no valid PEM certificates", config.MLFlow.CACertPath)
			}
			tlsConfig.RootCAs = caCertPool
			logger.Info("Loaded MLflow CA certificate", "path", config.MLFlow.CACertPath)
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

	// Configure auth token. Two modes are supported:
	//   1. Token file path (WithTokenPath) — re-read on each request, supports
	//      Kubernetes projected SA tokens that are rotated on disk by the kubelet.
	//   2. Static token (WithToken) — for local development without a token file.
	// At runtime, the token file takes precedence over the static token.
	tokenPath := config.MLFlow.TokenPath
	// Always configure the token path; resolveAuthToken handles transient
	// absence at request time (e.g. projected volume not yet mounted).
	if tokenPath != "" {
		client = client.WithTokenPath(tokenPath)
		logger.Info("MLflow auth token path configured (per-request reading)", "path", tokenPath)
	}
	if config.MLFlow.Token != "" {
		client = client.WithToken(config.MLFlow.Token)
		logger.Info("MLflow static auth token configured")
	}

	if config.IsOTELEnabled() {
		currentHTTPClient := client.GetHTTPClient()
		client = client.WithHTTPClient(&http.Client{
			Transport: otelhttp.NewTransport(currentHTTPClient.Transport),
			Timeout:   currentHTTPClient.Timeout,
		})
		logger.Info("Enabled OTEL transport for MLFlow client")
	}

	// Retain the configured workspace name even if the probe fails; PrepareClient
	// re-probes later when support is still unknown.
	if config.MLFlow.Workspace != "" {
		client = client.WithWorkspace(config.MLFlow.Workspace)
	}

	workspaceSupport := NewWorkspaceSupport()
	// Single probe at startup. A definitive enabled/disabled result is cached;
	// failure leaves support unknown for the next MLflow-dependent job to retry.
	if err := workspaceSupport.Resolve(context.Background(), client); err != nil {
		logger.Warn(
			"Could not probe MLflow workspace support during startup; will retry on the next MLflow-dependent job",
			"error", err.Error(),
			"workspace", config.MLFlow.Workspace,
		)
	} else {
		client = workspaceSupport.Apply(client)
		if workspaceSupport.Enabled() && config.MLFlow.Workspace != "" {
			logger.Info("MLflow workspace configured", "workspace", config.MLFlow.Workspace)
		}
	}

	logger.Info("MLFlow tracking enabled", "mlflow_experiment_url", client.GetExperimentsURL())

	return client, workspaceSupport, nil
}

func injectEvaluationJobTags(jobID string, evaluation *api.EvaluationJobConfig) []api.ExperimentTag {
	if evaluation.Experiment != nil {
		tags := evaluation.Experiment.Tags
		if tags == nil {
			tags = make([]api.ExperimentTag, 0)
		}

		tags = append(tags, api.ExperimentTag{
			Key:   "context",
			Value: "eval-hub",
		})

		if evaluation.Name != "" {
			tags = append(tags, api.ExperimentTag{
				Key:   "evaluation_job_name",
				Value: evaluation.Name,
			})
		}
		if evaluation.Description != nil {
			tags = append(tags, api.ExperimentTag{
				Key:   "evaluation_job_description",
				Value: *evaluation.Description,
			})
		}
		tags = append(tags, api.ExperimentTag{
			Key:   "evaluation_job_id",
			Value: jobID,
		})
		return tags
	}
	return []api.ExperimentTag{}
}

// HasExperimentName is true when the job config has a non-empty MLflow experiment name.
func HasExperimentName(jobConfig *api.EvaluationJobConfig) bool {
	return jobConfig.Experiment != nil && strings.TrimSpace(jobConfig.Experiment.Name) != ""
}

func GetOrCreateExperimentID(mlflowClient *mlflowclient.Client, workspaceSupport *WorkspaceSupport, jobConfig *api.EvaluationJobConfig, jobID string) (experimentID string, experimentURL string, err error) {
	if !HasExperimentName(jobConfig) {
		return "", "", nil
	}

	// if we get here then we have an experiment name so we need an MLFlow client

	if mlflowClient == nil {
		return "", "", serviceerrors.NewServiceError(messages.MLFlowRequiredForExperiment)
	}

	prepared, err := prepareMLFlowClient(mlflowClient, workspaceSupport)
	if err != nil {
		return "", "", serviceerrors.NewServiceError(messages.MLFlowRequestFailed, "Error", err.Error())
	}
	if err := prepared.EnsureWorkspace(); err != nil {
		return "", "", serviceerrors.NewServiceError(messages.MLFlowRequestFailed, "Error", err.Error())
	}

	tags := injectEvaluationJobTags(jobID, jobConfig)
	req := mlflowclient.CreateExperimentRequest{
		Name:             jobConfig.Experiment.Name,
		ArtifactLocation: jobConfig.Experiment.ArtifactLocation,
		Tags:             tags,
	}
	mlflowExperiment, err := prepared.GetOrCreateExperiment(&req)
	if err != nil {
		return "", "", serviceerrors.NewServiceError(messages.MLFlowRequestFailed, "Error", err.Error())
	}

	prepared.GetLogger().Info("Resolved experiment", "experiment_name", jobConfig.Experiment.Name, "experiment_id", mlflowExperiment.Experiment.ExperimentID)
	return mlflowExperiment.Experiment.ExperimentID, prepared.GetExperimentsURL(), nil
}

func prepareMLFlowClient(client *mlflowclient.Client, workspaceSupport *WorkspaceSupport) (*mlflowclient.Client, error) {
	if client == nil {
		return nil, fmt.Errorf("mlflow client does not exist")
	}
	if workspaceSupport == nil {
		return client, nil
	}
	return workspaceSupport.PrepareClient(client.Context(), client)
}
