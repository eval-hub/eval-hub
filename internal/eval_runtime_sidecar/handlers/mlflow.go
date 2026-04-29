package handlers

import (
	"fmt"
	"log/slog"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
	"github.com/eval-hub/eval-hub/internal/eval_runtime_sidecar/proxy"
)

// MLFlowTokenPathDefault is the default path for the MLflow bearer token in the container.
const MLFlowTokenPathDefault = "/var/run/secrets/mlflow/token"

// /api/2.0|3.0/mlflow/... and /api/2.0/mlflow-artifacts/... are proxied to mlflow.tracking_uri.
// (mlflow-artifacts is a separate top-level path on the tracking server, not under /mlflow/.)
const (
	mlflowAPIv2PathPrefix          = "/api/2.0/mlflow"
	mlflowAPIv3PathPrefix          = "/api/3.0/mlflow"
	mlflowAPIv2ArtifactsPathPrefix = "/api/2.0/mlflow-artifacts"
)

// isMLflowProxyPath returns true for the MLflow REST API roots, requiring an exact path or a
// subpath (prefix + "/") so names like /api/2.0/mlflowx do not match /api/2.0/mlflow.
func isMLflowProxyPath(path string) bool {
	return mlflowPathMatchesPrefix(path, mlflowAPIv2PathPrefix) ||
		mlflowPathMatchesPrefix(path, mlflowAPIv3PathPrefix) ||
		mlflowPathMatchesPrefix(path, mlflowAPIv2ArtifactsPathPrefix)
}

func mlflowPathMatchesPrefix(path, prefix string) bool {
	return path == prefix || strings.HasPrefix(path, prefix+"/")
}

func newMlflowProxy(config *config.Config, logger *slog.Logger) (*httputil.ReverseProxy, error) {
	mlflowTrackingURI := ""
	if config.MLFlow != nil {
		mlflowTrackingURI = strings.TrimSpace(config.MLFlow.TrackingURI)
	}
	if mlflowTrackingURI == "" {
		logger.Warn("mlflow.tracking_uri is not set in sidecar config")
		return nil, nil
	}
	mlflowHTTPClient, err := proxy.NewMLFlowHTTPClient(config, config.IsOTELEnabled(), logger)
	if err != nil {
		logger.Error("failed to create mlflow HTTP client", "error", err)
		return nil, fmt.Errorf("failed to create mlflow HTTP client: %w", err)
	}
	mlflowTarget, err := url.Parse(strings.TrimSuffix(mlflowTrackingURI, "/"))
	if err != nil {
		return nil, fmt.Errorf("invalid mlflow.tracking_uri: %w", err)
	}

	mlflowProxy := proxy.NewReverseProxy(mlflowTarget, mlflowHTTPClient, logger, nil)
	return mlflowProxy, nil
}
