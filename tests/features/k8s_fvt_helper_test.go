package features

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFVTKubeConfigPrefersKubeconfig(t *testing.T) {
	dir := t.TempDir()
	kubeconfig := filepath.Join(dir, "config")
	if err := os.WriteFile(kubeconfig, []byte(minimalKubeconfig), 0o600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}

	t.Setenv("KUBECONFIG", kubeconfig)

	cfg, err := loadFVTKubeConfig()
	if err != nil {
		t.Fatalf("loadFVTKubeConfig() error = %v", err)
	}
	if cfg.Host != "https://test-cluster.example:6443" {
		t.Fatalf("Host = %q, want https://test-cluster.example:6443", cfg.Host)
	}
}

func TestLoadFVTKubeConfigFailsWhenExplicitKubeconfigInvalid(t *testing.T) {
	t.Setenv("KUBECONFIG", filepath.Join(t.TempDir(), "missing-config"))

	_, err := loadFVTKubeConfig()
	if err == nil {
		t.Fatal("loadFVTKubeConfig() error = nil, want error for invalid KUBECONFIG")
	}
}

const minimalKubeconfig = `apiVersion: v1
kind: Config
clusters:
- cluster:
    server: https://test-cluster.example:6443
  name: test
contexts:
- context:
    cluster: test
    user: test
  name: test
current-context: test
users:
- name: test
  user:
    token: test-token
`
