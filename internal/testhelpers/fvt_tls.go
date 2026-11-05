package testhelpers

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/eval-hub/eval-hub/internal/safefile"
)

// NewFVTTransport adds the optional private CA bundle to system trust without
// modifying the default transport or disabling certificate verification.
func NewFVTTransport() (*http.Transport, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	caPath := os.Getenv("FVT_CA_CERT_PATH")
	if caPath == "" {
		return transport, nil
	}
	// Treat the configured parent directory as the trusted root. The bundle
	// name cannot traverse outside it, including through symlinks.
	pemData, err := safefile.ReadFile(filepath.Dir(caPath), filepath.Base(caPath))
	if err != nil {
		return nil, fmt.Errorf("read FVT_CA_CERT_PATH: %w", err)
	}
	roots, err := x509.SystemCertPool()
	if err != nil {
		return nil, fmt.Errorf("load system CA certificates: %w", err)
	}
	if !roots.AppendCertsFromPEM(pemData) {
		return nil, fmt.Errorf("FVT_CA_CERT_PATH %q contains no PEM certificates", caPath)
	}
	transport.TLSClientConfig.RootCAs = roots
	return transport, nil
}
