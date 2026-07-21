package ociclient

import (
	"encoding/base64"
	"net/url"
	"strings"
	"testing"
)

func FuzzParseDockerConfigJSON(f *testing.F) {
	f.Add([]byte(`{"auths":{"https://quay.io":{"auth":"dXNlcjpwYXNz"}}}`), "quay.io")
	f.Add([]byte(`{"auths":{"https://index.docker.io/v1/":{"username":"docker-user","password":"docker-pass"}}}`), "docker.io")
	f.Add([]byte(`{`), "quay.io")
	f.Add([]byte(`{}`), "quay.io")
	f.Add([]byte(`{"auths":{"https://quay.io":{"auth":"!!!"}}}`), "quay.io")
	f.Add([]byte(`{"auths":{}}`), "")

	f.Fuzz(func(t *testing.T, data []byte, registryHost string) {
		creds, err := ParseDockerConfigJSON(data, registryHost)
		if err != nil {
			if creds != (Credentials{}) {
				t.Fatalf("expected empty credentials on error, got %#v", creds)
			}
			return
		}
		if creds.Username == "" || creds.Password == "" {
			t.Fatalf("successful parse returned empty credentials: %#v", creds)
		}
	})
}

func FuzzCanonicalRegistryHost(f *testing.F) {
	f.Add("quay.io")
	f.Add("https://QUAY.IO/")
	f.Add("registry-1.docker.io")
	f.Add("https://index.docker.io/v1/")
	f.Add("http://registry.example:5000/v2")
	f.Add("")
	f.Add("://bad")

	f.Fuzz(func(t *testing.T, host string) {
		got := canonicalRegistryHost(host)
		if strings.Contains(got, "://") {
			t.Fatalf("canonicalRegistryHost(%q) = %q contains scheme", host, got)
		}
		if got != canonicalRegistryHost(host) {
			t.Fatalf("canonicalRegistryHost is not deterministic for %q", host)
		}
	})
}

func FuzzParseBearerRealm(f *testing.F) {
	f.Add(`Bearer realm="https://auth.example/token",service="registry",scope="repository:org/repo:pull"`)
	f.Add(`Bearer realm="https://auth.example/token"`)
	f.Add(`Bearer realm="https://auth.example/token?existing=1",service="registry"`)
	f.Add(`Basic realm="x"`)
	f.Add(`Bearer service="registry"`)
	f.Add("")
	f.Add(`Bearer realm="https://auth.example/token",service="reg,istry",scope="a,b"`)

	f.Fuzz(func(t *testing.T, header string) {
		got, err := parseBearerRealm(header)
		if err != nil {
			if got != "" {
				t.Fatalf("expected empty URL on error, got %q", got)
			}
			return
		}
		if got == "" {
			t.Fatal("successful parse returned empty realm URL")
		}
		// Realm is taken from the challenge as-is (plus optional query params).
		// Callers validate the URL separately via validateTokenRealmURL.
	})
}

func FuzzValidateTokenRealmURL(f *testing.F) {
	f.Add("https://quay.io", "https://quay.io/v2/token")
	f.Add("http://registry.local:5000", "http://registry.local:5000/token")
	f.Add("https://quay.io", "http://quay.io/v2/token")
	f.Add("https://quay.io", "ftp://quay.io/token")
	f.Add("https://quay.io", "://bad-url")
	f.Add("", "")

	f.Fuzz(func(t *testing.T, registry, nextURL string) {
		err := validateTokenRealmURL(registry, nextURL)
		parsed, parseErr := url.Parse(nextURL)
		if parseErr != nil {
			if err == nil {
				t.Fatal("expected error for unparseable token realm URL")
			}
			return
		}
		switch parsed.Scheme {
		case "https":
			if err != nil {
				t.Fatalf("unexpected error for https realm: %v", err)
			}
		case "http":
			if strings.HasPrefix(registry, "https://") {
				if err == nil {
					t.Fatal("expected error for http realm with https registry")
				}
			} else if err != nil {
				t.Fatalf("unexpected error for http realm with non-https registry: %v", err)
			}
		default:
			if err == nil {
				t.Fatalf("expected error for unsupported scheme %q", parsed.Scheme)
			}
		}
	})
}

func FuzzParseAuthParam(f *testing.F) {
	f.Add(`realm="https://auth.example/token"`)
	f.Add(`service=registry`)
	f.Add("invalid")
	f.Add("")
	f.Add(`key="value with spaces"`)
	f.Add(base64.StdEncoding.EncodeToString([]byte("user:pass")))

	f.Fuzz(func(t *testing.T, s string) {
		key, value, ok := parseAuthParam(s)
		if !ok {
			if key != "" || value != "" {
				t.Fatalf("failed parse returned non-empty values: %q %q", key, value)
			}
			return
		}
		if !strings.Contains(s, "=") {
			t.Fatal("successful parse without '=' in input")
		}
		// Empty keys (e.g. "=0") are accepted by the parser; callers ignore unknown keys.
		_ = key
		_ = value
	})
}
