package server

import (
	"encoding/json"
	"testing"
)

func TestModelInputUnmarshalAuthSecret(t *testing.T) {
	t.Parallel()

	var m ModelInput
	if err := json.Unmarshal([]byte(`{
		"url": "http://model:8080",
		"name": "test-model",
		"auth_secret": "my-secret"
	}`), &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if m.URL != "http://model:8080" {
		t.Errorf("url = %q", m.URL)
	}
	if m.Auth == nil || m.Auth.SecretRef != "my-secret" {
		t.Errorf("auth = %#v, want secret_ref my-secret", m.Auth)
	}
}

func TestModelInputUnmarshalAuth(t *testing.T) {
	t.Parallel()

	var m ModelInput
	if err := json.Unmarshal([]byte(`{
		"url": "http://model:8080",
		"name": "test-model",
		"auth": { "secret_ref": "api-secret" }
	}`), &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if m.Auth == nil || m.Auth.SecretRef != "api-secret" {
		t.Errorf("auth = %#v, want secret_ref api-secret", m.Auth)
	}
}

func TestModelInputUnmarshalAuthSecretDoesNotOverrideAuth(t *testing.T) {
	t.Parallel()

	var m ModelInput
	if err := json.Unmarshal([]byte(`{
		"url": "http://model:8080",
		"name": "test-model",
		"auth": { "secret_ref": "api-secret" },
		"auth_secret": "legacy-secret"
	}`), &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if m.Auth == nil || m.Auth.SecretRef != "api-secret" {
		t.Errorf("auth = %#v, want api-secret to win over auth_secret", m.Auth)
	}
}

func TestModelInputUnmarshalParameters(t *testing.T) {
	t.Parallel()

	var m ModelInput
	if err := json.Unmarshal([]byte(`{
		"url": "http://model:8080",
		"name": "test-model",
		"parameters": { "temperature": 0.5 }
	}`), &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if m.Parameters["temperature"] != 0.5 {
		t.Errorf("parameters = %v", m.Parameters)
	}
}
