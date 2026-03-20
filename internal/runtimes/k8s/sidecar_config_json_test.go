package k8s

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/config"
)

func TestMarshalSidecarForJobPod_IncludesOCI(t *testing.T) {
	svc := &config.Config{
		Sidecar: &config.SidecarConfig{
			Port:    8080,
			BaseURL: "http://localhost:8080",
			EvalHub: &config.EvalHubClientConfig{
				BaseURL:     "https://eval.example",
				HTTPTimeout: 30 * time.Second,
			},
			OCI: &config.SidecarOCIConfig{
				HTTPTimeout:        30 * time.Second,
				InsecureSkipVerify: true,
			},
		},
	}
	b, err := marshalSidecarForJobPod(svc, nil)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	oci, ok := m["oci"].(map[string]any)
	if !ok {
		t.Fatalf("expected oci object in sidecar_config.json, got: %s", string(b))
	}
	if oci["http_timeout"].(float64) != float64(30*time.Second) {
		t.Fatalf("oci.http_timeout: %v", oci["http_timeout"])
	}
	if oci["insecure_skip_verify"] != true {
		t.Fatalf("oci.insecure_skip_verify: %v", oci["insecure_skip_verify"])
	}
}
