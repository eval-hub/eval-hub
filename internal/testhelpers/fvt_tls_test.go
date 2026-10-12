package testhelpers

import (
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFVTPrivateCA(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	caPath := filepath.Join(t.TempDir(), "ca.pem")
	cert := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	if err := os.WriteFile(caPath, cert, 0o600); err != nil {
		t.Fatal(err)
	}
	// Clone initializes Go's lazy HTTP/2 defaults before we snapshot them.
	_ = http.DefaultTransport.(*http.Transport).Clone()
	defaultTLS := http.DefaultTransport.(*http.Transport).TLSClientConfig

	t.Run("system trust rejects unknown CA", func(t *testing.T) {
		t.Setenv("FVT_CA_CERT_PATH", "")
		transport, err := NewFVTTransport()
		if err != nil {
			t.Fatal(err)
		}
		defer transport.CloseIdleConnections()
		client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
		resp, err := client.Get(server.URL)
		if err == nil {
			_ = resp.Body.Close()
			t.Fatal("untrusted certificate was accepted")
		}
	})
	t.Run("configured CA trusts server", func(t *testing.T) {
		t.Setenv("FVT_CA_CERT_PATH", caPath)
		transport, err := NewFVTTransport()
		if err != nil {
			t.Fatal(err)
		}
		defer transport.CloseIdleConnections()
		client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
		resp, err := client.Get(server.URL)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("status = %d, want 204", resp.StatusCode)
		}
	})
	t.Run("configured CA still verifies hostname", func(t *testing.T) {
		t.Setenv("FVT_CA_CERT_PATH", caPath)
		transport, err := NewFVTTransport()
		if err != nil {
			t.Fatal(err)
		}
		defer transport.CloseIdleConnections()
		transport.TLSClientConfig.ServerName = "wrong.example"
		client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
		resp, err := client.Get(server.URL)
		if err == nil {
			_ = resp.Body.Close()
			t.Fatal("certificate for a different hostname was accepted")
		}
	})
	if http.DefaultTransport.(*http.Transport).TLSClientConfig != defaultTLS {
		t.Fatal("default transport TLS configuration was modified")
	}
}

func TestFVTInvalidCA(t *testing.T) {
	invalidPath := filepath.Join(t.TempDir(), "invalid.pem")
	if err := os.WriteFile(invalidPath, []byte("not a certificate"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(t.TempDir(), "missing.pem"), invalidPath} {
		t.Run(filepath.Base(path), func(t *testing.T) {
			t.Setenv("FVT_CA_CERT_PATH", path)
			if _, err := NewFVTTransport(); err == nil {
				t.Fatal("expected CA configuration error")
			}
		})
	}
}
