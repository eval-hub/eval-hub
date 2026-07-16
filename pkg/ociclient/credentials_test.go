package ociclient

import (
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
	_, err := DockerConfigJSONFromSecret(map[string][]byte{"other": []byte("x")})
	if err == nil {
		t.Fatal("expected error for missing dockerconfigjson key")
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
	}{
		{name: "invalid json", data: []byte("{"), host: "quay.io"},
		{name: "missing auths", data: []byte("{}"), host: "quay.io"},
		{name: "missing registry", data: []byte(`{"auths":{"https://quay.io":{"username":"u","password":"p"}}}`), host: ""},
		{name: "unknown registry", data: []byte(`{"auths":{"https://quay.io":{"username":"u","password":"p"}}}`), host: "unknown.example"},
		{name: "invalid auth encoding", data: []byte(`{"auths":{"https://quay.io":{"auth":"!!!"}}}`), host: "quay.io"},
		{name: "missing credentials", data: []byte(`{"auths":{"https://quay.io":{}}}`), host: "quay.io"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := ParseDockerConfigJSON(tc.data, tc.host); err == nil {
				t.Fatal("expected error")
			}
		})
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
