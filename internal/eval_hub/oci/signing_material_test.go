package oci

import (
	"bytes"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func signingDirectoryURI(path string) string {
	return (&url.URL{Scheme: "file", Path: path}).String()
}

func writeSigningFile(t *testing.T, directory, name string, content []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(directory, name), content, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestResolveLocalSigningMaterialExactBytes(t *testing.T) {
	t.Setenv("COSIGN_PASSWORD", "must-not-be-used")
	for _, passphrase := range []*string{nil, new(""), new(" \tpassword\r\n ")} {
		directory := t.TempDir()
		key := []byte("private key bytes\n")
		writeSigningFile(t, directory, "private.key", key)
		want := []byte{}
		if passphrase != nil {
			want = []byte(*passphrase)
			writeSigningFile(t, directory, "passphrase.key", want)
		}
		material, err := ResolveLocalSigningMaterial(signingDirectoryURI(directory))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(material.PrivateKey, key) || !bytes.Equal(material.Passphrase, want) || material.Passphrase == nil {
			t.Fatal("key/passphrase bytes changed or empty passphrase was not explicit")
		}
	}
}

func TestResolveLocalSigningMaterialBounds(t *testing.T) {
	for _, tt := range []struct {
		name            string
		key, passphrase int
		valid           bool
	}{
		{"exact key limit", maxSigningMaterialBytes, 0, true},
		{"exact passphrase limit", 0, maxSigningMaterialBytes, true},
		{"exact combined limit", 16, maxSigningMaterialBytes - 16, true},
		{"key over limit", maxSigningMaterialBytes + 1, 0, false},
		{"passphrase over limit", 0, maxSigningMaterialBytes + 1, false},
		{"combined overflow", 16, maxSigningMaterialBytes - 15, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			directory := t.TempDir()
			writeSigningFile(t, directory, "private.key", bytes.Repeat([]byte{'k'}, tt.key))
			writeSigningFile(t, directory, "passphrase.key", bytes.Repeat([]byte{'p'}, tt.passphrase))
			material, err := ResolveLocalSigningMaterial(signingDirectoryURI(directory))
			if tt.valid {
				if err != nil || len(material.PrivateKey) != tt.key || len(material.Passphrase) != tt.passphrase {
					t.Fatalf("valid material rejected/truncated: %v", err)
				}
			} else if err == nil || material != nil || !strings.Contains(err.Error(), "combined 1 MiB limit") {
				t.Fatalf("expected bounded-read failure, got material=%v, error=%v", material != nil, err)
			}
		})
	}
}

func TestResolveLocalSigningMaterialConfinement(t *testing.T) {
	for _, name := range []string{"private.key", "passphrase.key"} {
		t.Run(name, func(t *testing.T) {
			directory := t.TempDir()
			outside := t.TempDir()
			writeSigningFile(t, outside, "sensitive", []byte("do-not-expose"))
			if name == "passphrase.key" {
				writeSigningFile(t, directory, "private.key", []byte("key"))
			}
			if err := os.Symlink(filepath.Join(outside, "sensitive"), filepath.Join(directory, name)); err != nil {
				t.Fatal(err)
			}
			material, err := ResolveLocalSigningMaterial(signingDirectoryURI(directory))
			if err == nil || material != nil || strings.Contains(err.Error(), outside) || strings.Contains(err.Error(), "do-not-expose") {
				t.Fatalf("expected sanitized confinement failure: %v", err)
			}
		})
	}
}

func TestResolveLocalSigningMaterialSymlinksAndDecoding(t *testing.T) {
	base := t.TempDir()
	directory := filepath.Join(base, "keys%2F with space")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	writeSigningFile(t, directory, "actual.key", []byte("key"))
	if err := os.Symlink("actual.key", filepath.Join(directory, "private.key")); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "selected-directory")
	if err := os.Symlink(directory, link); err != nil {
		t.Fatal(err)
	}
	// Unrelated entries and public keys must not be read.
	if err := os.Mkdir(filepath.Join(directory, "public.key"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{directory, link} {
		material, err := ResolveLocalSigningMaterial(signingDirectoryURI(path))
		if err != nil || string(material.PrivateKey) != "key" {
			t.Fatalf("allowed symlink or once-decoded path failed: %v", err)
		}
	}
}

func TestResolveLocalSigningMaterialFailures(t *testing.T) {
	for _, tt := range []string{"missing directory", "missing key", "key directory", "passphrase directory", "dangling passphrase", "unreadable key", "unreadable passphrase"} {
		t.Run(tt, func(t *testing.T) {
			directory := t.TempDir()
			switch tt {
			case "missing directory":
				directory = filepath.Join(directory, "missing")
			case "missing key":
			case "key directory":
				if err := os.Mkdir(filepath.Join(directory, "private.key"), 0700); err != nil {
					t.Fatal(err)
				}
			default:
				writeSigningFile(t, directory, "private.key", []byte("sensitive-key"))
				switch tt {
				case "passphrase directory":
					if err := os.Mkdir(filepath.Join(directory, "passphrase.key"), 0700); err != nil {
						t.Fatal(err)
					}
				case "dangling passphrase":
					if err := os.Symlink("missing", filepath.Join(directory, "passphrase.key")); err != nil {
						t.Fatal(err)
					}
				default:
					if os.Geteuid() == 0 {
						t.Skip("root bypasses file read permissions")
					}
					name := "private.key"
					if tt == "unreadable passphrase" {
						name = "passphrase.key"
						writeSigningFile(t, directory, name, []byte("sensitive-password"))
					}
					if err := os.Chmod(filepath.Join(directory, name), 0000); err != nil {
						t.Fatal(err)
					}
				}
			}
			material, err := ResolveLocalSigningMaterial(signingDirectoryURI(directory))
			if err == nil || material != nil {
				t.Fatal("expected material read failure")
			}
			for _, secret := range []string{directory, "sensitive-key", "sensitive-password"} {
				if strings.Contains(err.Error(), secret) {
					t.Fatalf("error leaks signing material/path: %v", err)
				}
			}
		})
	}
	if material, err := ResolveLocalSigningMaterial("file://host/keys"); err == nil || material != nil {
		t.Fatal("invalid URI accepted by resolver")
	}
}
