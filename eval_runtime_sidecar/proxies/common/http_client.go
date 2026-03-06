package common

import (
	"crypto/tls"
	"log/slog"
	"net/http"
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
