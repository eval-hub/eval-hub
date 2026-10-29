package servingtls

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func unsetControls(t *testing.T) {
	t.Helper()
	for _, key := range []string{"TLS_REQUIRED", "TLS_MIN_VERSION", "TLS_CIPHERS", "TLS_CURVES"} {
		old, set := os.LookupEnv(key)
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if set {
				_ = os.Setenv(key, old)
			} else {
				_ = os.Unsetenv(key)
			}
		})
	}
}

func servingFiles(t *testing.T) (string, string, *x509.CertPool) {
	t.Helper()
	server := httptest.NewTLSServer(http.NotFoundHandler())
	defer server.Close()
	certificate := server.TLS.Certificates[0]
	key, err := x509.MarshalPKCS8PrivateKey(certificate.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	certFile, keyFile := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Certificate[0]}), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key}), 0600); err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	return certFile, keyFile, roots
}

func TestOptionalHTTPAndValidation(t *testing.T) {
	unsetControls(t)
	cfg, err := FromEnv("", "")
	if err != nil || cfg != nil {
		t.Fatalf("optional HTTP: %#v %v", cfg, err)
	}
	cert, key, _ := servingFiles(t)
	for _, tt := range []struct{ name, key, value, cert, keyFile string }{
		{"required", "TLS_REQUIRED", "true", "", ""},
		{"invalid bool", "TLS_REQUIRED", "typo", cert, key},
		{"old version", "TLS_MIN_VERSION", "1.1", cert, key},
		{"unknown version", "TLS_MIN_VERSION", "VersionTLS13", cert, key},
		{"plaintext with policy", "TLS_MIN_VERSION", "1.3", "", ""},
		{"unsupported suite", "TLS_CIPHERS", "nonsense", cert, key},
		{"TLS13 suite", "TLS_CIPHERS", "TLS_AES_128_GCM_SHA256", cert, key},
		{"empty suites", "TLS_CIPHERS", "", cert, key},
		{"duplicate suite", "TLS_CIPHERS", "TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256", cert, key},
		{"unknown group", "TLS_CURVES", "4588", cert, key},
		{"empty groups", "TLS_CURVES", "", cert, key},
		{"duplicate groups", "TLS_CURVES", "P256,P256", cert, key},
		{"TLS12 hybrid only", "TLS_CURVES", "X25519MLKEM768", cert, key},
		{"partial cert", "TLS_REQUIRED", "false", cert, ""},
		{"partial key", "TLS_REQUIRED", "false", "", key},
		{"missing files", "TLS_REQUIRED", "false", filepath.Join(t.TempDir(), "missing"), key},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(tt.key, tt.value)
			cfg, err := FromEnv(tt.cert, tt.keyFile)
			if err == nil || cfg != nil {
				t.Fatalf("invalid settings allowed: %#v, %v", cfg, err)
			}
		})
	}
	t.Run("ignored TLS12 suites forbidden in TLS13 mode", func(t *testing.T) {
		t.Setenv("TLS_MIN_VERSION", "1.3")
		t.Setenv("TLS_CIPHERS", "TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256")
		if _, err := FromEnv(cert, key); err == nil {
			t.Fatal("irrelevant cipher settings silently ignored")
		}
	})
	t.Run("malformed material", func(t *testing.T) {
		broken := filepath.Join(t.TempDir(), "broken.pem")
		if err := os.WriteFile(broken, []byte("not PEM"), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := FromEnv(broken, key); err == nil {
			t.Fatal("invalid certificate accepted")
		}
	})
}

func TestNegotiatedVersionsCiphersAndGroups(t *testing.T) {
	unsetControls(t)
	cert, key, roots := servingFiles(t)
	for _, tt := range []struct {
		name, min, ciphers, curves string
		version                    uint16
		group                      tls.CurveID
		cipher                     uint16
	}{
		{"TLS12", "1.2", "TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256", "P256", tls.VersionTLS12, tls.CurveP256, tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256},
		{"TLS13 hybrid", "1.3", "", "X25519MLKEM768", tls.VersionTLS13, tls.X25519MLKEM768, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("TLS_MIN_VERSION", tt.min)
			t.Setenv("TLS_CURVES", tt.curves)
			t.Setenv("TLS_REQUIRED", "true")
			if tt.ciphers != "" {
				t.Setenv("TLS_CIPHERS", tt.ciphers)
			}
			cfg, err := FromEnv(cert, key)
			if err != nil {
				t.Fatal(err)
			}
			server := httptest.NewUnstartedServer(http.NotFoundHandler())
			server.TLS = cfg
			server.StartTLS()
			defer server.Close()
			clientConfig := &tls.Config{RootCAs: roots, MinVersion: tt.version, MaxVersion: tt.version, CurvePreferences: []tls.CurveID{tt.group}, NextProtos: []string{"h2"}}
			if tt.cipher != 0 {
				clientConfig.CipherSuites = []uint16{tt.cipher}
			}
			conn, err := tls.Dial("tcp", server.Listener.Addr().String(), clientConfig)
			if err != nil {
				t.Fatal(err)
			}
			state := conn.ConnectionState()
			_ = conn.Close()
			if state.Version != tt.version || state.CurveID != tt.group || state.NegotiatedProtocol != "h2" || (tt.cipher != 0 && state.CipherSuite != tt.cipher) {
				t.Fatalf("wrong negotiated settings: %#v", state)
			}
			bad := clientConfig.Clone()
			if tt.version == tls.VersionTLS13 {
				bad.MinVersion = tls.VersionTLS12
				bad.MaxVersion = tls.VersionTLS12
			} else {
				bad.CurvePreferences = []tls.CurveID{tls.CurveP384}
			}
			rejected, err := tls.Dial("tcp", server.Listener.Addr().String(), bad)
			if err == nil {
				_ = rejected.Close()
				t.Fatal("disallowed version/group negotiated")
			}
		})
	}
}
