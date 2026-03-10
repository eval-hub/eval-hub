package common

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// NewHTTPClient creates an HTTP client with the given timeout and TLS config.
// If isOTELEnabled is true, the transport is wrapped with OTEL instrumentation.
func NewHTTPClient(timeout time.Duration, tlsConfig *tls.Config, isOTELEnabled bool, logger *slog.Logger, transportLabel string) *http.Client {
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
func BuildTLSConfig(caCertPath string, insecureSkipVerify bool, logger *slog.Logger, certLabel string) (*tls.Config, error) {
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
