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
