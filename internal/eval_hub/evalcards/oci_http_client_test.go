package evalcards

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"log/slog"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
)

func TestNewOCIHTTPClientDefaults(t *testing.T) {
	t.Parallel()

	client, err := NewOCIHTTPClient(nil, false, nil)
	if err != nil {
		t.Fatalf("NewOCIHTTPClient() err = %v", err)
	}
	if client.Timeout != defaultOCIHTTPTimeout {
		t.Fatalf("timeout = %v, want %v", client.Timeout, defaultOCIHTTPTimeout)
	}
}

func TestNewOCIHTTPClientAppliesConfig(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Sidecar: &config.SidecarConfig{
			OCI: &config.SidecarOCIConfig{
				HTTPTimeout:        45 * time.Second,
				InsecureSkipVerify: true,
			},
		},
	}
	client, err := NewOCIHTTPClient(cfg, false, nil)
	if err != nil {
		t.Fatalf("NewOCIHTTPClient() err = %v", err)
	}
	if client.Timeout != 45*time.Second {
		t.Fatalf("timeout = %v, want 45s", client.Timeout)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok || transport.TLSClientConfig == nil || !transport.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("expected insecure TLS config from sidecar.oci settings")
	}
}

func TestBuildOCIHTTPClientTLSMissingCAUsesSystemRoots(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	tlsConfig, err := buildOCIHTTPClientTLS(filepath.Join(t.TempDir(), "missing-ca.crt"), false, logger)
	if err != nil {
		t.Fatalf("buildOCIHTTPClientTLS() err = %v", err)
	}
	if tlsConfig != nil {
		t.Fatalf("tlsConfig = %#v, want nil when CA file is absent", tlsConfig)
	}
}

func TestBuildOCIHTTPClientTLSInvalidPEM(t *testing.T) {
	t.Parallel()

	caPath := filepath.Join(t.TempDir(), "bad-ca.crt")
	if err := os.WriteFile(caPath, []byte("not-a-cert"), 0o600); err != nil {
		t.Fatalf("WriteFile() err = %v", err)
	}
	if _, err := buildOCIHTTPClientTLS(caPath, false, nil); err == nil {
		t.Fatal("expected error for invalid CA PEM")
	}
}

func TestBuildOCIHTTPClientTLSValidCA(t *testing.T) {
	t.Parallel()

	caPath := writeTestCACert(t)
	tlsConfig, err := buildOCIHTTPClientTLS(caPath, false, nil)
	if err != nil {
		t.Fatalf("buildOCIHTTPClientTLS() err = %v", err)
	}
	if tlsConfig == nil || tlsConfig.RootCAs == nil {
		t.Fatal("expected TLS config with custom root CAs")
	}
}

func TestNewOCIHTTPClientInvalidCA(t *testing.T) {
	t.Parallel()

	caPath := filepath.Join(t.TempDir(), "bad-ca.crt")
	if err := os.WriteFile(caPath, []byte("not-a-cert"), 0o600); err != nil {
		t.Fatalf("WriteFile() err = %v", err)
	}
	if _, err := NewOCIHTTPClient(&config.Config{
		Sidecar: &config.SidecarConfig{
			OCI: &config.SidecarOCIConfig{CACertPath: caPath},
		},
	}, false, nil); err == nil {
		t.Fatal("expected invalid CA error")
	}
}

func TestNewOCIHTTPClientWithOTELEnabled(t *testing.T) {
	t.Parallel()

	client, err := NewOCIHTTPClient(&config.Config{
		OTEL: &config.OTELConfig{Enabled: true},
	}, true, nil)
	if err != nil {
		t.Fatalf("NewOCIHTTPClient() err = %v", err)
	}
	if client.Transport == nil {
		t.Fatal("expected transport")
	}
}

func TestBuildOCIHTTPClientTLSReadError(t *testing.T) {
	t.Parallel()

	if _, err := buildOCIHTTPClientTLS(t.TempDir(), false, nil); err == nil {
		t.Fatal("expected read error when CA path is a directory")
	}
}

func writeTestCACert(t *testing.T) string {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey() err = %v", err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "eval-hub-test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("CreateCertificate() err = %v", err)
	}
	path := filepath.Join(t.TempDir(), "ca.crt")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("Create() err = %v", err)
	}
	defer f.Close()
	if err := pem.Encode(f, &pem.Block{Type: "CERTIFICATE", Bytes: der}); err != nil {
		t.Fatalf("pem.Encode() err = %v", err)
	}
	return path
}
