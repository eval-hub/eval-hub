package proxy

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
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

// buildTLSConfig creates a TLS config from CA cert path and insecure flag.
// When insecureSkipVerify is false and caCertPath is non-empty, the CA PEM is loaded into RootCAs
// (production Eval Hub / MLflow / OCI behavior on cluster).
// When insecureSkipVerify is true, custom CA files are not read: verification is off, so loading a CA
// would not affect trust and skipping avoids failing on missing paths in local/test environments.
// When caCertPath is empty and insecureSkipVerify is false, system roots are used (default *tls.Config).
func buildTLSConfig(caCertPath string, insecureSkipVerify bool, logger *slog.Logger, certLabel string) (*tls.Config, error) {
	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS12,
		MaxVersion: tls.VersionTLS13,
	}
	if caCertPath != "" && !insecureSkipVerify {
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
	caCertPath := ""
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

// resolveCACertPathIfPresent returns the first non-empty path that exists on disk, or empty if none exist.
func resolveCACertPathIfPresent(paths []string, logger *slog.Logger) string {
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if _, err := os.Stat(p); err == nil {
			return p
		}
		if logger != nil {
			logger.Debug("model proxy: CA path not found, trying next", "path", p)
		}
	}
	return ""
}

// NewModelProxyHTTPClient builds an HTTP client for the model upstream (TLS, timeout).
// Returns (nil, nil) when sidecar model is not configured or URL is empty.
func NewModelProxyHTTPClient(serviceConfig *config.Config, isOTELEnabled bool, logger *slog.Logger) (*http.Client, error) {
	var mp *config.SidecarModelConfig
	if serviceConfig != nil && serviceConfig.Sidecar != nil {
		mp = serviceConfig.Sidecar.Model
	}
	if mp == nil || strings.TrimSpace(mp.URL) == "" {
		return nil, nil
	}

	timeout := DefaultHTTPTimeout
	if mp.HTTPTimeout > 0 {
		timeout = mp.HTTPTimeout
	}

	caPath := resolveCACertPathIfPresent([]string{mp.AuthCACertPath}, logger)
	tlsConfig, err := buildTLSConfig(caPath, mp.InsecureSkipVerify, logger, "Model")
	if err != nil {
		return nil, err
	}

	client := newHTTPClient(timeout, tlsConfig, isOTELEnabled, logger, "Model")
	return client, nil
}
