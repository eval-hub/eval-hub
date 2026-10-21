package ociclient

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestParseDockerConfigJSON(t *testing.T) {
	data := []byte(`{
		"auths": {
			"https://quay.io": {
				"auth": "dXNlcjpwYXNz"
			}
		}
	}`)
	creds, err := ParseDockerConfigJSON(data, "quay.io")
	if err != nil {
		t.Fatalf("ParseDockerConfigJSON() err = %v", err)
	}
	if creds.Username != "user" || creds.Password != "pass" {
		t.Fatalf("creds = %#v", creds)
	}
}

func TestParseDockerConfigJSONDockerAlias(t *testing.T) {
	data := []byte(`{
		"auths": {
			"https://index.docker.io/v1/": {
				"username": "docker-user",
				"password": "docker-pass"
			}
		}
	}`)
	creds, err := ParseDockerConfigJSON(data, "docker.io")
	if err != nil {
		t.Fatalf("ParseDockerConfigJSON() err = %v", err)
	}
	if creds.Username != "docker-user" || creds.Password != "docker-pass" {
		t.Fatalf("creds = %#v", creds)
	}
}

func TestDockerConfigJSONFromSecret(t *testing.T) {
	for _, data := range []map[string][]byte{nil, {}, {"other": []byte("x")}, {dockerConfigJSONKey: {}}} {
		_, err := DockerConfigJSONFromSecret(data)
		if err == nil || err.Error() != "docker configuration is missing" {
			t.Fatalf("expected missing Docker configuration, got %v", err)
		}
	}
	raw, err := DockerConfigJSONFromSecret(map[string][]byte{dockerConfigJSONKey: []byte(`{"auths":{}}`)})
	if err != nil || len(raw) == 0 {
		t.Fatalf("DockerConfigJSONFromSecret() = %q err=%v", raw, err)
	}
}

func TestNormalizeRegistryHost(t *testing.T) {
	if got := NormalizeRegistryHost("quay.io"); got != "https://quay.io" {
		t.Fatalf("got %q", got)
	}
	if got := NormalizeRegistryHost("http://registry.example:5000"); got != "http://registry.example:5000" {
		t.Fatalf("got %q", got)
	}
}

func TestParseDockerConfigJSONErrors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		data []byte
		host string
		want string
	}{
		{name: "invalid json", data: []byte("{"), host: "quay.io", want: "registry credentials are invalid"},
		{name: "invalid json shape", data: []byte(`{"auths":[]}`), host: "quay.io", want: "registry credentials are invalid"},
		{name: "missing auths", data: []byte("{}"), host: "quay.io", want: "registry credentials are missing"},
		{name: "empty auths", data: []byte(`{"auths":{}}`), host: "quay.io", want: "registry credentials are missing"},
		{name: "missing registry", data: []byte(`{"auths":{"https://quay.io":{"username":"u","password":"p"}}}`), host: ""},
		{name: "unknown registry", data: []byte(`{"auths":{"https://quay.io":{"username":"u","password":"p"}}}`), host: "unknown.example", want: "registry credentials are missing"},
		{name: "invalid auth encoding", data: []byte(`{"auths":{"https://quay.io":{"auth":"!!!"}}}`), host: "quay.io", want: "registry credentials are invalid"},
		{name: "missing credentials", data: []byte(`{"auths":{"https://quay.io":{}}}`), host: "quay.io", want: "registry credentials are invalid"},
		{name: "incomplete decoded credentials", data: []byte(`{"auths":{"quay.io":{"auth":"dXNlcjo="}}}`), host: "quay.io", want: "registry credentials are invalid"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := ParseDockerConfigJSON(tc.data, tc.host)
			if err == nil {
				t.Fatal("expected error")
			}
			if tc.want != "" && !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected message containing %q, got %v", tc.want, err)
			}
		})
	}
}

func TestParseDockerConfigJSONPreservesUnderlyingErrors(t *testing.T) {
	_, err := ParseDockerConfigJSON([]byte("{"), "quay.io")
	var syntax *json.SyntaxError
	if !errors.As(err, &syntax) {
		t.Fatalf("expected underlying JSON syntax error, got %v", err)
	}
	_, err = ParseDockerConfigJSON([]byte(`{"auths":{"quay.io":{"auth":"!!!"}}}`), "quay.io")
	var corrupt base64.CorruptInputError
	if !errors.As(err, &corrupt) {
		t.Fatalf("expected underlying base64 error, got %v", err)
	}
}

func TestCanonicalRegistryHost(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"quay.io":                         "quay.io",
		"https://QUAY.IO/":                "quay.io",
		"registry-1.docker.io":            "docker.io",
		"https://index.docker.io/v1/":     "docker.io",
		"http://registry.example:5000/v2": "registry.example:5000",
	}
	for input, want := range cases {
		if got := canonicalRegistryHost(input); got != want {
			t.Fatalf("canonicalRegistryHost(%q) = %q, want %q", input, got, want)
		}
	}
	if canonicalRegistryHost("") != "" {
		t.Fatal("expected empty host for blank input")
	}
}

func TestDockerConfigJSONFromSecretNilData(t *testing.T) {
	if _, err := DockerConfigJSONFromSecret(nil); err == nil {
		t.Fatal("expected error for nil secret data")
	}
}
