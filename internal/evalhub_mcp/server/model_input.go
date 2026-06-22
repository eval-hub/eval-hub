package server

import (
	"encoding/json"

	"github.com/eval-hub/eval-hub/pkg/api"
)

// ModelInput wraps api.ModelRef for submit_evaluation. It accepts the REST API
// JSON shape and the legacy auth_secret field as an alias for auth.secret_ref.
type ModelInput struct {
	api.ModelRef
	// AuthSecret is a legacy MCP alias for auth.secret_ref. It is merged into Auth
	// during unmarshaling and is not sent to the eval-hub API.
	AuthSecret string `json:"auth_secret,omitempty" jsonschema:"Kubernetes secret reference (alias for auth.secret_ref)"`
}

func (m *ModelInput) UnmarshalJSON(data []byte) error {
	type plain ModelInput
	if err := json.Unmarshal(data, (*plain)(m)); err != nil {
		return err
	}
	if m.AuthSecret != "" && (m.Auth == nil || m.Auth.SecretRef == "") {
		m.Auth = &api.ModelAuth{SecretRef: m.AuthSecret}
	}
	m.AuthSecret = ""
	return nil
}
