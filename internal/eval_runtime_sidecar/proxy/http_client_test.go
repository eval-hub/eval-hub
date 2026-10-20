package proxy

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
)

func TestSchemeRequiresTLS(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		url    string
		expect bool
	}{
		{"empty defaults to true", "", true},
		{"http does not require TLS", "http://localhost:8080", false},
		{"https requires TLS", "https://evalhub.example.com", true},
		{"no scheme defaults to true", "example.com", true},
		{"unparseable defaults to true", "://bad", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := schemeRequiresTLS(tc.url); got != tc.expect {
				t.Errorf("schemeRequiresTLS(%q) = %v, want %v", tc.url, got, tc.expect)
			}
		})
	}
}

func TestNewEvalHubHTTPClient(t *testing.T) {
	logger := slog.Default()

	t.Run("returns nil when config is nil", func(t *testing.T) {
		client, err := NewEvalHubHTTPClient(nil, false, logger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if client != nil {
			t.Error("expected nil client when config is nil")
		}
	})

	t.Run("returns nil when Sidecar is nil", func(t *testing.T) {
		cfg := &config.Config{}
		client, err := NewEvalHubHTTPClient(cfg, false, logger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if client != nil {
			t.Error("expected nil client when Sidecar is nil")
		}
	})

	t.Run("returns client when Sidecar and EvalHub set", func(t *testing.T) {
		cfg := &config.Config{
			Sidecar: &config.SidecarConfig{
				EvalHub: &config.EvalHubClientConfig{
					InsecureSkipVerify: true,
				},
			},
		}
		client, err := NewEvalHubHTTPClient(cfg, false, logger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if client == nil {
			t.Fatal("expected non-nil client")
		}
		if client.Timeout == 0 {
			t.Error("expected non-zero timeout")
		}
	})

	t.Run("http URL skips TLS config without insecure_skip_verify", func(t *testing.T) {
		cfg := &config.Config{
			Sidecar: &config.SidecarConfig{
				EvalHub: &config.EvalHubClientConfig{
					BaseURL: "http://localhost:8080",
				},
			},
		}
		client, err := NewEvalHubHTTPClient(cfg, false, logger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if client == nil {
			t.Fatal("expected non-nil client")
		}
	})

	t.Run("https URL with insecure_skip_verify does not require CA cert", func(t *testing.T) {
		cfg := &config.Config{
			Sidecar: &config.SidecarConfig{
				EvalHub: &config.EvalHubClientConfig{
					BaseURL:            "https://evalhub.example.com",
					InsecureSkipVerify: true,
				},
			},
		}
		client, err := NewEvalHubHTTPClient(cfg, false, logger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if client == nil {
			t.Fatal("expected non-nil client")
		}
	})
}

func TestNewMLFlowHTTPClient(t *testing.T) {
	logger := slog.Default()

	t.Run("returns nil when config is nil", func(t *testing.T) {
		client, err := NewMLFlowHTTPClient(nil, false, logger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if client != nil {
			t.Error("expected nil client when config is nil")
		}
	})

	t.Run("returns nil when MLFlow is nil", func(t *testing.T) {
		cfg := &config.Config{}
		client, err := NewMLFlowHTTPClient(cfg, false, logger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if client != nil {
			t.Error("expected nil client when MLFlow is nil")
		}
	})

	t.Run("returns nil when TrackingURI is empty", func(t *testing.T) {
		cfg := &config.Config{
			MLFlow: &config.MLFlowConfig{},
		}
		client, err := NewMLFlowHTTPClient(cfg, false, logger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if client != nil {
			t.Error("expected nil client when TrackingURI is empty")
		}
	})

	t.Run("returns client when MLFlow and TrackingURI set", func(t *testing.T) {
		cfg := &config.Config{
			MLFlow: &config.MLFlowConfig{
				TrackingURI: "https://mlflow.example.com",
			},
		}
		client, err := NewMLFlowHTTPClient(cfg, false, logger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if client == nil {
			t.Fatal("expected non-nil client")
		}
		if client.Timeout == 0 {
			t.Error("expected non-zero timeout")
		}
	})

	t.Run("http TrackingURI skips TLS config", func(t *testing.T) {
		cfg := &config.Config{
			MLFlow: &config.MLFlowConfig{
				TrackingURI: "http://localhost:5000",
			},
		}
		client, err := NewMLFlowHTTPClient(cfg, false, logger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if client == nil {
			t.Fatal("expected non-nil client")
		}
	})
}

func TestNewModelHTTPClient(t *testing.T) {
	logger := slog.Default()

	t.Run("returns nil when config is nil", func(t *testing.T) {
		client, err := NewModelHTTPClient(nil, false, logger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if client != nil {
			t.Error("expected nil client when config is nil")
		}
	})

	t.Run("returns nil when Sidecar is nil", func(t *testing.T) {
		cfg := &config.Config{}
		client, err := NewModelHTTPClient(cfg, false, logger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if client != nil {
			t.Error("expected nil client when Sidecar is nil")
		}
	})

	t.Run("returns client with defaults when Model is nil", func(t *testing.T) {
		cfg := &config.Config{Sidecar: &config.SidecarConfig{}}
		client, err := NewModelHTTPClient(cfg, false, logger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if client == nil {
			t.Fatal("expected non-nil client")
		}
		if client.Timeout != DefaultHTTPTimeout {
			t.Errorf("timeout = %v, want %v", client.Timeout, DefaultHTTPTimeout)
		}
	})

	t.Run("http URL without CA cert skips TLS config", func(t *testing.T) {
		cfg := &config.Config{
			Sidecar: &config.SidecarConfig{
				Model: &config.SidecarModelConfig{
					URL: "http://localhost:8080",
				},
			},
		}
		client, err := NewModelHTTPClient(cfg, false, logger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if client == nil {
			t.Fatal("expected non-nil client")
		}
	})

	t.Run("http URL with CA cert builds TLS config for multi-destination routing", func(t *testing.T) {
		previousToken := modelSACachedToken.Swap(nil)
		t.Cleanup(func() { modelSACachedToken.Store(previousToken) })
		model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}))
		defer model.Close()
		kfp := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer test-sa-token" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.WriteHeader(http.StatusAccepted)
		}))
		defer kfp.Close()
		for _, withCA := range []bool{true, false} {
			t.Run(fmt.Sprintf("with_CA=%t", withCA), func(t *testing.T) {
				secretDir := t.TempDir()
				files := map[string][]byte{
					"kfp_url":      []byte(kfp.URL),
					"kfp_sa_token": {},
				}
				if withCA {
					files["ca_cert"] = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: kfp.Certificate().Raw})
				}
				for name, data := range files {
					if err := os.WriteFile(filepath.Join(secretDir, name), data, 0o600); err != nil {
						t.Fatal(err)
					}
				}
				saTokenPath := filepath.Join(t.TempDir(), "token")
				if err := os.WriteFile(saTokenPath, []byte("test-sa-token"), 0o600); err != nil {
					t.Fatal(err)
				}
				cfg := &config.Config{Sidecar: &config.SidecarConfig{Model: &config.SidecarModelConfig{
					URL: model.URL, AuthSecretMountPath: secretDir,
				}}}
				client, err := NewModelHTTPClient(cfg, false, logger)
				if err != nil {
					t.Fatalf("NewModelHTTPClient: %v", err)
				}
				t.Cleanup(client.CloseIdleConnections)
				target, err := url.Parse(model.URL)
				if err != nil {
					t.Fatal(err)
				}
				proxy := NewModelReverseProxy(target, client, logger, secretDir, saTokenPath)
				for _, useRef := range []bool{false, true} {
					req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
					wantStatus := http.StatusNoContent
					if useRef {
						req.Header.Set("Authorization", "Bearer kfp_sa_token:ref")
						wantStatus = http.StatusBadGateway
						if withCA {
							wantStatus = http.StatusAccepted
						}
					}
					rr := httptest.NewRecorder()
					proxy.ServeHTTP(rr, req)
					if rr.Code != wantStatus {
						t.Fatalf("ref=%t: status = %d, want %d; body = %s", useRef, rr.Code, wantStatus, rr.Body.String())
					}
				}
			})
		}
	})

	t.Run("https URL with insecure_skip_verify", func(t *testing.T) {
		cfg := &config.Config{
			Sidecar: &config.SidecarConfig{
				Model: &config.SidecarModelConfig{
					URL:                "https://model.example.com",
					InsecureSkipVerify: true,
				},
			},
		}
		client, err := NewModelHTTPClient(cfg, false, logger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if client == nil {
			t.Fatal("expected non-nil client")
		}
	})
}

func TestNewOCIHTTPClient(t *testing.T) {
	logger := slog.Default()

	t.Run("returns nil when config is nil", func(t *testing.T) {
		client, err := NewOCIHTTPClient(nil, false, logger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if client != nil {
			t.Error("expected nil client when config is nil")
		}
	})

	t.Run("returns nil when Sidecar is nil", func(t *testing.T) {
		cfg := &config.Config{}
		client, err := NewOCIHTTPClient(cfg, false, logger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if client != nil {
			t.Error("expected nil client when Sidecar is nil")
		}
	})

	t.Run("returns client with defaults when OCI is nil", func(t *testing.T) {
		cfg := &config.Config{Sidecar: &config.SidecarConfig{}}
		client, err := NewOCIHTTPClient(cfg, false, logger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if client == nil {
			t.Fatal("expected non-nil client")
		}
		if client.Timeout != DefaultHTTPTimeout {
			t.Errorf("timeout = %v, want %v", client.Timeout, DefaultHTTPTimeout)
		}
	})

	t.Run("uses custom timeout from OCI config", func(t *testing.T) {
		cfg := &config.Config{
			Sidecar: &config.SidecarConfig{
				OCI: &config.SidecarOCIConfig{
					HTTPTimeout: 60_000_000_000,
				},
			},
		}
		client, err := NewOCIHTTPClient(cfg, false, logger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if client == nil {
			t.Fatal("expected non-nil client")
		}
	})
}

func TestBuildTLSConfig_LoadsCACert(t *testing.T) {
	t.Parallel()
	caPath := writeProxyTestCACert(t)
	tlsCfg, err := buildTLSConfig(caPath, false, slog.Default(), "test")
	if err != nil {
		t.Fatalf("buildTLSConfig: %v", err)
	}
	if tlsCfg == nil || tlsCfg.RootCAs == nil {
		t.Fatal("expected TLS config with RootCAs")
	}
}

func TestBuildTLSConfig_PreservesSystemRoots(t *testing.T) {
	t.Parallel()
	caPath := writeProxyTestCACert(t)
	wantRoots, err := x509.SystemCertPool()
	if err != nil {
		t.Fatalf("SystemCertPool: %v", err)
	}
	caPEM, err := os.ReadFile(caPath)
	if err != nil {
		t.Fatal(err)
	}
	if !wantRoots.AppendCertsFromPEM(caPEM) {
		t.Fatal("failed to parse test CA")
	}
	tlsCfg, err := buildTLSConfig(caPath, false, slog.Default(), "test")
	if err != nil {
		t.Fatalf("buildTLSConfig: %v", err)
	}
	if !tlsCfg.RootCAs.Equal(wantRoots) {
		t.Fatal("custom CA must extend the system trust store so other HTTPS destinations remain trusted")
	}
}

func writeProxyTestCACert(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "proxy-test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("CreateCertificate: %v", err)
	}
	path := filepath.Join(t.TempDir(), "ca.crt")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer func() { _ = f.Close() }()
	if err := pem.Encode(f, &pem.Block{Type: "CERTIFICATE", Bytes: der}); err != nil {
		t.Fatalf("pem.Encode: %v", err)
	}
	return path
}
