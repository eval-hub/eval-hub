package proxy

import (
	"crypto/fips140"
	"encoding/pem"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
)

func TestClientConstructorsVerifyCertificates(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer server.Close()
	for name, constructor := range map[string]func(*config.Config, bool, *slog.Logger) (*http.Client, error){
		"EvalHub": NewEvalHubHTTPClient, "Model": NewModelHTTPClient, "MLflow": NewMLFlowHTTPClient, "OCI": NewOCIHTTPClient,
	} {
		t.Run(name, func(t *testing.T) {
			for _, trusted := range []bool{true, false} {
				dir := t.TempDir()
				caPath := filepath.Join(dir, "ca_cert")
				ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
				if !trusted {
					var err error
					ca, err = os.ReadFile(writeProxyTestCACert(t))
					if err != nil {
						t.Fatal(err)
					}
				}
				if err := os.WriteFile(caPath, ca, 0600); err != nil {
					t.Fatal(err)
				}
				cfg := &config.Config{
					Sidecar: &config.SidecarConfig{
						EvalHub: &config.EvalHubClientConfig{BaseURL: server.URL, CACertPath: caPath},
						Model:   &config.SidecarModelConfig{URL: server.URL, AuthSecretMountPath: dir},
						OCI:     &config.SidecarOCIConfig{CACertPath: caPath},
					},
					MLFlow: &config.MLFlowConfig{TrackingURI: server.URL, CACertPath: caPath},
				}
				client, err := constructor(cfg, false, slog.Default())
				if err != nil {
					t.Fatal(err)
				}
				defer client.CloseIdleConnections()
				response, err := client.Get(server.URL)
				if response != nil {
					_ = response.Body.Close()
				}
				if trusted && err != nil {
					t.Fatalf("trusted certificate rejected: %v", err)
				}
				if !trusted && err == nil {
					t.Fatal("untrusted certificate accepted")
				}
			}
		})
	}
}

func TestFIPSClientConstructorsRejectVerificationBypass(t *testing.T) {
	if !fips140.Enabled() {
		command := exec.Command(os.Args[0], "-test.run=^TestFIPSClientConstructorsRejectVerificationBypass$")
		command.Env = append(os.Environ(), "GODEBUG=fips140=on")
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("FIPS-enabled constructor test: %v\n%s", err, output)
		}
		return
	}
	cfg := &config.Config{Sidecar: &config.SidecarConfig{
		EvalHub: &config.EvalHubClientConfig{BaseURL: "https://eval-hub.example", InsecureSkipVerify: true},
		Model:   &config.SidecarModelConfig{URL: "https://model.example", InsecureSkipVerify: true},
	}}
	for name, constructor := range map[string]func(*config.Config, bool, *slog.Logger) (*http.Client, error){"EvalHub": NewEvalHubHTTPClient, "Model": NewModelHTTPClient} {
		t.Run(name, func(t *testing.T) {
			client, err := constructor(cfg, false, slog.Default())
			if err == nil || client != nil || !strings.Contains(err.Error(), "FIPS") {
				t.Fatalf("FIPS bypass must fail explicitly at construction: %#v %v", client, err)
			}
		})
	}
}
