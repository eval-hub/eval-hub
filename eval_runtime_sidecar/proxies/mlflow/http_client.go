package mlflow

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/eval-hub/eval-hub/eval_runtime_sidecar/proxies/common"
	"github.com/eval-hub/eval-hub/internal/config"
)

const defaultHTTPTimeout = 30 * time.Second

// NewHTTPClient creates an HTTP client for the MLflow service from config.
// Returns (nil, nil) when MLFlow is not configured or TrackingURI is empty.
func NewHTTPClient(serviceConfig *config.Config, isOTELEnabled bool, logger *slog.Logger) (*http.Client, error) {
	if serviceConfig == nil || serviceConfig.MLFlow == nil || serviceConfig.MLFlow.TrackingURI == "" {
		logger.Warn("MLFlow tracking URI is not set, skipping MLFlow client creation")
		return nil, nil
	}

	mlflowConfig := serviceConfig.MLFlow

	timeout := defaultHTTPTimeout
	if mlflowConfig.HTTPTimeout > 0 {
		timeout = mlflowConfig.HTTPTimeout
	}

	tlsConfig, err := common.BuildTLSConfig(mlflowConfig.CACertPath, mlflowConfig.InsecureSkipVerify, logger, "MLflow")
	if err != nil {
		return nil, err
	}

	client := common.NewHTTPClient(timeout, tlsConfig, isOTELEnabled, logger, "MLflow")
	return client, nil
}
