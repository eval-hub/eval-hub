package common

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log/slog"
	"os"
)

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
