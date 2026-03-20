package proxy

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCanonicalRegistryHost(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"https://registry.example:5000/", "registry.example:5000"},
		{"http://REGISTRY.EXAMPLE", "registry.example"},
		{"registry.io", "registry.io"},
		{"https://index.docker.io/v1/", "docker.io"},
		{"index.docker.io", "docker.io"},
		{"registry-1.docker.io", "docker.io"},
		{"https://docker.io/v2/", "docker.io"},
	}
	for _, tt := range tests {
		if got := canonicalRegistryHost(tt.in); got != tt.want {
			t.Errorf("canonicalRegistryHost(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestLoadTokenProducerFromOCISecret_noSingleEntryFallback(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.json")
	// Single auth entry for quay.io only; job targets ghcr.io — must not use quay credentials.
	const secret = `{"auths":{"quay.io":{"username":"u","password":"p"}}}`
	if err := os.WriteFile(p, []byte(secret), 0o600); err != nil {
		t.Fatal(err)
	}
	client := &http.Client{}
	_, err := LoadTokenProducerFromOCISecret(p, "https://ghcr.io", "org/repo", client)
	if err == nil {
		t.Fatal("expected error when auths has no matching registry")
	}
	if !strings.Contains(err.Error(), "no auth for registry") || !strings.Contains(err.Error(), "ghcr.io") {
		t.Fatalf("error = %v", err)
	}
}

func TestLoadTokenProducerFromOCISecret_canonicalMatch(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.json")
	const secret = `{"auths":{"https://registry.example:5000/v2/":{"username":"u","password":"p"}}}`
	if err := os.WriteFile(p, []byte(secret), 0o600); err != nil {
		t.Fatal(err)
	}
	tp, err := LoadTokenProducerFromOCISecret(p, "http://registry.example:5000", "org/repo", &http.Client{})
	if err != nil {
		t.Fatal(err)
	}
	if tp.Username != "u" || tp.Password != "p" {
		t.Fatalf("TokenProducer = %+v", tp)
	}
}

func TestLoadTokenProducerFromOCISecret_dockerAlias(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.json")
	const secret = `{"auths":{"https://index.docker.io/v1/":{"username":"u","password":"p"}}}`
	if err := os.WriteFile(p, []byte(secret), 0o600); err != nil {
		t.Fatal(err)
	}
	tp, err := LoadTokenProducerFromOCISecret(p, "docker.io", "org/repo", &http.Client{})
	if err != nil {
		t.Fatal(err)
	}
	if tp.Username != "u" {
		t.Fatalf("expected docker.io to match index.docker.io auths key, err=%v", err)
	}
}
