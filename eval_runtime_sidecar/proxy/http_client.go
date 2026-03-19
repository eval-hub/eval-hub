package proxy

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/eval-hub/eval-hub/internal/config"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

const DefaultCACertPath = "/etc/pki/ca-trust/source/anchors/service-ca.crt"
const DefaultInsecureSkipVerify = false
const DefaultHTTPTimeout = 30 * time.Second

// NewHTTPClient creates an HTTP client with the given timeout and TLS config.
// If isOTELEnabled is true, the transport is wrapped with OTEL instrumentation.
func newHTTPClient(timeout time.Duration, tlsConfig *tls.Config, isOTELEnabled bool, logger *slog.Logger, transportLabel string) *http.Client {
	transport := &http.Transport{}
	if tlsConfig != nil {
		transport.TLSClientConfig = tlsConfig
	}
	client := &http.Client{
		Timeout:   timeout,
		Transport: transport,
	}
	if isOTELEnabled {
		client = &http.Client{
			Transport: otelhttp.NewTransport(client.Transport),
			Timeout:   client.Timeout,
		}
		logger.Info("Enabled OTEL transport", "label", transportLabel)
	}
	return client
}

// BuildTLSConfig creates a TLS config from CA cert path and insecure flag.
// Returns nil if both caCertPath is empty and insecureSkipVerify is false (default secure).
func buildTLSConfig(caCertPath string, insecureSkipVerify bool, logger *slog.Logger, certLabel string) (*tls.Config, error) {
	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS12,
		MaxVersion: tls.VersionTLS13,
	}
	if caCertPath != "" {
		caCert, err := os.ReadFile(caCertPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read %s CA certificate at %s: %w", certLabel, caCertPath, err)
		}
		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to parse %s CA certificate at %s: file contains no valid PEM certificates", certLabel, caCertPath)
		}
		tlsConfig.RootCAs = caCertPool
		logger.Info("Loaded CA certificate", "label", certLabel, "path", caCertPath)
	}
	if insecureSkipVerify {
		tlsConfig.InsecureSkipVerify = true
		logger.Warn("TLS certificate verification is disabled", "label", certLabel)
	}
	return tlsConfig, nil
}

// NewHTTPClient creates an HTTP client for the eval-hub service from config.
// Returns (nil, nil) when Sidecar.EvalHub is not configured.
func NewEvalHubHTTPClient(config *config.Config, isOTELEnabled bool, logger *slog.Logger) (*http.Client, error) {
	if config == nil || config.Sidecar == nil {
		return nil, nil
	}
	cfg := config.Sidecar.EvalHub

	timeout := DefaultHTTPTimeout
	if cfg != nil && cfg.HTTPTimeout > 0 {
		timeout = cfg.HTTPTimeout
	}

	caCertPath := DefaultCACertPath
	if cfg != nil && cfg.CACertPath != "" {
		caCertPath = cfg.CACertPath
	}
	insecureSkipVerify := DefaultInsecureSkipVerify
	if cfg != nil && cfg.InsecureSkipVerify {
		insecureSkipVerify = cfg.InsecureSkipVerify
	}

	var tlsConfig *tls.Config
	var err error
	if cfg != nil {
		tlsConfig, err = buildTLSConfig(caCertPath, insecureSkipVerify, logger, "EvalHub")
		if err != nil {
			return nil, err
		}
	}

	client := newHTTPClient(timeout, tlsConfig, isOTELEnabled, logger, "EvalHub")
	return client, nil
}

// NewHTTPClient creates an HTTP client for the MLflow service from config.
// Returns (nil, nil) when MLFlow is not configured or TrackingURI is empty.
func NewMLFlowHTTPClient(serviceConfig *config.Config, isOTELEnabled bool, logger *slog.Logger) (*http.Client, error) {
	if serviceConfig == nil || serviceConfig.MLFlow == nil || serviceConfig.MLFlow.TrackingURI == "" {
		logger.Warn("MLFlow tracking URI is not set, skipping MLFlow client creation")
		return nil, nil
	}
	// Job pod: MLFlow TLS/timeout come from sidecar_config.json (mlflow) merged into MLFlow at load.
	mlflowConfig := serviceConfig.MLFlow

	timeout := DefaultHTTPTimeout
	if mlflowConfig.HTTPTimeout > 0 {
		timeout = mlflowConfig.HTTPTimeout
	}

	tlsConfig, err := buildTLSConfig(mlflowConfig.CACertPath, mlflowConfig.InsecureSkipVerify, logger, "MLflow")
	if err != nil {
		return nil, err
	}

	client := newHTTPClient(timeout, tlsConfig, isOTELEnabled, logger, "MLflow")
	return client, nil
}

// NewOCIHTTPClient creates an HTTP client for the OCI registry from config (TLS, timeout).
// Registry host comes from the job spec, not sidecar.oci.host; Host may be empty.
// Returns (nil, nil) only when Sidecar.OCI is nil.
func NewOCIHTTPClient(serviceConfig *config.Config, isOTELEnabled bool, logger *slog.Logger) (*http.Client, error) {
	if serviceConfig == nil || serviceConfig.Sidecar == nil {
		return nil, nil
	}
	ociConfig := serviceConfig.Sidecar.OCI
	timeout := DefaultHTTPTimeout
	if ociConfig != nil && ociConfig.HTTPTimeout > 0 {
		timeout = ociConfig.HTTPTimeout
	}
	caCertPath := DefaultCACertPath
	if ociConfig != nil && ociConfig.CACertPath != "" {
		caCertPath = ociConfig.CACertPath
	}
	insecureSkipVerify := DefaultInsecureSkipVerify
	if ociConfig != nil && ociConfig.InsecureSkipVerify {
		insecureSkipVerify = ociConfig.InsecureSkipVerify
	}
	tlsConfig, err := buildTLSConfig(caCertPath, insecureSkipVerify, logger, "OCI")
	if err != nil {
		return nil, err
	}
	client := newHTTPClient(timeout, tlsConfig, isOTELEnabled, logger, "OCI")
	return client, nil
}
