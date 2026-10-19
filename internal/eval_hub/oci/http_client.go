package oci

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
	"github.com/eval-hub/eval-hub/internal/safefile"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

const (
	defaultOCIHTTPTimeout = 30 * time.Second
	defaultOCICACertPath  = "/etc/pki/ca-trust/source/anchors/service-ca.crt"
)

// NewHTTPClient creates an HTTP client for OCI registry export using optional sidecar.oci TLS
// settings. Cluster registries often use private CAs; this mirrors the sidecar OCI proxy TLS
// configuration for eval-hub's outbound export calls. TLS verification is always enabled.
func NewHTTPClient(serviceConfig *config.Config, isOTELEnabled bool, logger *slog.Logger) (*http.Client, error) {
	timeout := defaultOCIHTTPTimeout
	caCertPath := ""
	if serviceConfig != nil && serviceConfig.Sidecar != nil && serviceConfig.Sidecar.OCI != nil {
		oci := serviceConfig.Sidecar.OCI
		if oci.HTTPTimeout > 0 {
			timeout = oci.HTTPTimeout
		}
		caCertPath = oci.CACertPath
	}
	tlsConfig, err := buildOCIHTTPClientTLS(caCertPath, logger)
	if err != nil {
		return nil, err
	}
	transport := http.RoundTripper(&http.Transport{})
	if tlsConfig != nil {
		transport = &http.Transport{TLSClientConfig: tlsConfig}
	}
	if isOTELEnabled {
		transport = otelhttp.NewTransport(transport)
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
	}, nil
}

// buildOCIHTTPClientTLS loads registry TLS settings for the export HTTP client. It falls back to
// the cluster service CA bundle and, when absent, to system roots so export works in dev and prod.
func buildOCIHTTPClientTLS(caCertPath string, logger *slog.Logger) (*tls.Config, error) {
	if caCertPath == "" {
		caCertPath = defaultOCICACertPath
	}
	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS12,
	}
	caPEM, err := safefile.ReadFile(filepath.Dir(caCertPath), filepath.Base(caCertPath))
	if err != nil {
		if os.IsNotExist(err) {
			if logger != nil {
				logger.Info("OCI CA cert absent, using system roots", "path", caCertPath)
			}
			return nil, nil
		}
		return nil, fmt.Errorf("read oci ca cert %q: %w", caCertPath, err)
	}
	pool, err := x509.SystemCertPool()
	if err != nil {
		pool = x509.NewCertPool()
	}
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("parse oci ca cert %q", caCertPath)
	}
	tlsConfig.RootCAs = pool
	return tlsConfig, nil
}
