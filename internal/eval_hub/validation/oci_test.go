package validation

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/eval-hub/eval-hub/pkg/api"
)

func TestOCISigningRequestContract(t *testing.T) {
	validate := newTestValidator(t)
	for _, tt := range []struct {
		name, signing string
		valid         bool
	}{
		{"omitted", "", true},
		{"configured", `,"signing":{"type":"key-pair","secret_ref":"my-cosign-secret"}`, true},
		{"null", `,"signing":null`, true},
		{"no secret", `,"signing":{"type":"key-pair"}`, true},
		{"missing method", `,"signing":{"secret_ref":"key"}`, false},
		{"null method", `,"signing":{"type":null}`, false},
		{"empty method", `,"signing":{"type":""}`, false},
		{"keyless deferred", `,"signing":{"type":"keyless"}`, false},
		{"empty", `,"signing":{}`, false},
		{"null secret", `,"signing":{"type":"key-pair","secret_ref":null}`, true},
		{"empty secret", `,"signing":{"type":"key-pair","secret_ref":""}`, false},
		{"spaces secret", `,"signing":{"type":"key-pair","secret_ref":"  "}`, false},
		{"blank secret", `,"signing":{"type":"key-pair","secret_ref":" \t"}`, false},
		{"wrong type", `,"signing":{"type":"key-pair","secret_ref":42}`, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			raw := `{"oci":{"coordinates":{"oci_host":"quay.io","oci_repository":"org/results"}` + tt.signing + `}}`
			var exports api.EvaluationExports
			err := json.Unmarshal([]byte(raw), &exports)
			if err == nil {
				err = validate.Struct(exports)
			}
			if (err == nil) != tt.valid {
				t.Fatalf("valid=%v, error=%v", tt.valid, err)
			}
			if !tt.valid {
				return
			}
			job := api.EvaluationJobResource{EvaluationJobConfig: api.EvaluationJobConfig{Exports: &exports}}
			encoded, err := json.Marshal(job)
			if err != nil {
				t.Fatal(err)
			}
			var restored api.EvaluationJobResource
			if err := json.Unmarshal(encoded, &restored); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(encoded), `"results"`) {
				t.Fatalf("configuration produced results: %s", encoded)
			}
			if exports.OCI.Signing == nil {
				if strings.Contains(string(encoded), `"signing"`) {
					t.Fatal("omitted signing was emitted")
				}
			} else {
				if !reflect.DeepEqual(restored.Exports.OCI.Signing, exports.OCI.Signing) {
					t.Fatal("signing configuration lost")
				}
				if exports.OCI.Signing.SecretRef == nil && strings.Contains(string(encoded), `"secret_ref"`) {
					t.Fatal("absent secret reference emitted")
				}
			}
		})
	}
}

func TestOCIResultsContract(t *testing.T) {
	validate := newTestValidator(t)
	artifact := `{"oci_digest":"sha256:abc","oci_reference":"quay.io/org/results@sha256:abc"}`
	for _, tt := range []struct {
		name, raw string
		valid     bool
	}{
		{"omitted", `{}`, true},
		{"unavailable", `{"oci":{}}`, true},
		{"card", `{"oci":{"evaluation_card":` + artifact + `}}`, true},
		{"bundle", `{"oci":{"evaluation_bundle":` + artifact + `}}`, true},
		{"null oci", `{"oci":null}`, true},
		{"null card", `{"oci":{"evaluation_card":null}}`, true},
		{"null bundle", `{"oci":{"evaluation_bundle":null}}`, true},
		{"null signing", `{"oci":{"signing":null}}`, true},
		{"null status", `{"oci":{"status":null}}`, true},
		{"signing without status", `{"oci":{"signing":{"type":"key-pair"}}}`, true},
		{"empty card", `{"oci":{"evaluation_card":{}}}`, false},
		{"incomplete bundle", `{"oci":{"evaluation_bundle":{"oci_digest":"sha256:abc"}}}`, false},
		{"null digest", `{"oci":{"evaluation_card":{"oci_digest":null,"oci_reference":"ref"}}}`, false},
		{"blank reference", `{"oci":{"evaluation_card":{"oci_digest":"digest","oci_reference":" \t"}}}`, false},
		{"empty signing", `{"oci":{"signing":{}}}`, false},
		{"null signing type", `{"oci":{"signing":{"type":null}}}`, false},
		{"empty status", `{"oci":{"status":{}}}`, false},
		{"missing state", `{"oci":{"status":{"message":{"message":"Processing","message_code":"processing"}}}}`, false},
		{"null state", `{"oci":{"status":{"state":null,"message":{"message":"Processing","message_code":"processing"}}}}`, false},
		{"missing message", `{"oci":{"status":{"state":"completed"}}}`, false},
		{"null message", `{"oci":{"status":{"state":"completed","message":null}}}`, false},
		{"empty message", `{"oci":{"status":{"state":"completed","message":{}}}}`, false},
		{"missing message code", `{"oci":{"status":{"state":"completed","message":{"message":"Completed"}}}}`, false},
		{"missing message text", `{"oci":{"status":{"state":"completed","message":{"message_code":"completed"}}}}`, false},
		{"null message code", `{"oci":{"status":{"state":"completed","message":{"message":"Completed","message_code":null}}}}`, false},
		{"null message text", `{"oci":{"status":{"state":"completed","message":{"message":null,"message_code":"completed"}}}}`, false},
		{"empty message code", `{"oci":{"status":{"state":"completed","message":{"message":"Completed","message_code":""}}}}`, false},
		{"empty message text", `{"oci":{"status":{"state":"completed","message":{"message":"","message_code":"completed"}}}}`, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var results api.EvaluationJobResults
			err := json.Unmarshal([]byte(tt.raw), &results)
			if err == nil {
				err = validate.Struct(results)
			}
			if (err == nil) != tt.valid {
				t.Fatalf("valid=%v, error=%v", tt.valid, err)
			}
			if tt.valid {
				encoded, err := json.Marshal(results)
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(string(encoded), "null") {
					t.Fatalf("null optional object emitted: %s", encoded)
				}
				var restored api.EvaluationJobResults
				if err := json.Unmarshal(encoded, &restored); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(results, restored) {
					t.Fatalf("OCI results lost on round trip: %s", encoded)
				}
			}
			if tt.name == "omitted" || tt.name == "null oci" {
				data, _ := json.Marshal(results)
				if string(data) != `{}` {
					t.Fatalf("unset results emitted: %s", data)
				}
			}
		})
	}
}

func TestOCIProcessingStates(t *testing.T) {
	validate := newTestValidator(t)
	for _, tt := range []struct {
		state api.OCIProcessingState
		valid bool
	}{
		{api.OCIProcessingPending, true},
		{api.OCIProcessingGeneratingEvaluationCard, true},
		{api.OCIProcessingGeneratingEvaluationBundle, true},
		{api.OCIProcessingSigning, true},
		{api.OCIProcessingFailed, true},
		{api.OCIProcessingCompleted, true},
		{"signed", false},
		{"unsigned", false},
		{"unknown", false},
		{"", false},
	} {
		t.Run(string(tt.state), func(t *testing.T) {
			// Status is valid independently of artifact and signing outputs.
			results := api.EvaluationJobResults{OCI: &api.EvaluationOCIResults{
				Status: &api.OCIProcessingStatus{State: tt.state, Message: &api.MessageInfo{
					Message: "OCI processing", MessageCode: "oci_processing",
				}},
			}}
			if err := validate.Struct(results); (err == nil) != tt.valid {
				t.Fatalf("valid=%v, error=%v", tt.valid, err)
			}
			if !tt.valid {
				return
			}
			encoded, err := json.Marshal(results)
			if err != nil {
				t.Fatal(err)
			}
			var restored api.EvaluationJobResults
			if err := json.Unmarshal(encoded, &restored); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(results, restored) {
				t.Fatalf("OCI status lost on round trip: %s", encoded)
			}
		})
	}
}

func TestOCIResultsSigningMethods(t *testing.T) {
	validate := newTestValidator(t)
	for _, method := range []string{"key-pair", "keyless", ""} {
		t.Run(method, func(t *testing.T) {
			result := api.EvaluationOCIResults{Signing: &api.OCISigningResult{Type: method}}
			if err := validate.Struct(result); (err == nil) != (method == "key-pair") {
				t.Fatalf("method=%q, error=%v", method, err)
			}
		})
	}
}

func TestOCISigningStillRequiresCoordinates(t *testing.T) {
	validate := newTestValidator(t)
	for _, raw := range []string{
		`{"signing":{"type":"key-pair","secret_ref":"key"}}`,
		`{"coordinates":{},"signing":{"type":"key-pair","secret_ref":"key"}}`,
	} {
		var exports api.EvaluationExportsOCI
		if err := json.Unmarshal([]byte(raw), &exports); err != nil {
			t.Fatal(err)
		}
		if err := validate.Struct(exports); err == nil {
			t.Fatal("signing configuration bypassed required OCI coordinates")
		}
	}
}
