package server

import (
	"encoding/json"
	"testing"

	"github.com/eval-hub/eval-hub/pkg/api"
)

func TestModelRefUnmarshalAuth(t *testing.T) {
	t.Parallel()

	var m api.ModelRef
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

func TestModelRefUnmarshalParameters(t *testing.T) {
	t.Parallel()

	var m api.ModelRef
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
