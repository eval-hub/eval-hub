package handlers_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/executioncontext"
	"github.com/eval-hub/eval-hub/internal/eval_hub/serialization"
	"github.com/eval-hub/eval-hub/internal/eval_hub/validation"
	"github.com/eval-hub/eval-hub/pkg/api"
)

func TestEvaluationJobConfigHardwareConfigRoundTrip(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	validate := validation.NewValidator()
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-hwp", logger, "test-user", "test-tenant")

	t.Run("omits empty namespace in response", func(t *testing.T) {
		t.Parallel()
		body := []byte(`{
			"name":"test-job",
			"model":{"url":"http://test.com","name":"model"},
			"benchmarks":[{
				"id":"b1",
				"provider_id":"provider-1",
				"hardware_config":{
					"hardware_profile_ref":{
						"name":"cpu-optimized-profile"
					}
				}
			}]
		}`)

		cfg := &api.EvaluationJobConfig{}
		if err := serialization.Unmarshal(validate, ctx, body, cfg); err != nil {
			t.Fatalf("unmarshal request: %v", err)
		}
		if cfg.Benchmarks[0].HardwareConfig.HardwareProfileRef.Namespace != "" {
			t.Fatalf("namespace = %q, want empty when omitted from request", cfg.Benchmarks[0].HardwareConfig.HardwareProfileRef.Namespace)
		}

		job := &api.EvaluationJobResource{
			Resource:            api.EvaluationResource{Resource: api.Resource{ID: "job-1"}},
			EvaluationJobConfig: *cfg,
		}
		encoded, err := json.Marshal(job)
		if err != nil {
			t.Fatalf("marshal job: %v", err)
		}

		var response map[string]any
		if err := json.Unmarshal(encoded, &response); err != nil {
			t.Fatalf("unmarshal response: %v", err)
		}
		benchmarks, ok := response["benchmarks"].([]any)
		if !ok || len(benchmarks) != 1 {
			t.Fatalf("unexpected benchmarks in response: %s", string(encoded))
		}
		benchmark, ok := benchmarks[0].(map[string]any)
		if !ok {
			t.Fatalf("unexpected benchmark shape in response: %s", string(encoded))
		}
		hardwareConfig, ok := benchmark["hardware_config"].(map[string]any)
		if !ok {
			t.Fatalf("unexpected hardware_config in response: %s", string(encoded))
		}
		profileRef, ok := hardwareConfig["hardware_profile_ref"].(map[string]any)
		if !ok {
			t.Fatalf("unexpected hardware_profile_ref in response: %s", string(encoded))
		}
		if profileRef["name"] != "cpu-optimized-profile" {
			t.Fatalf("name = %v, want cpu-optimized-profile", profileRef["name"])
		}
		if _, hasNamespace := profileRef["namespace"]; hasNamespace {
			t.Fatalf("namespace should be omitted from response, got: %s", string(encoded))
		}
	})

	t.Run("preserves explicit namespace in response", func(t *testing.T) {
		t.Parallel()
		body := []byte(`{
			"name":"test-job",
			"model":{"url":"http://test.com","name":"model"},
			"benchmarks":[{
				"id":"b1",
				"provider_id":"provider-1",
				"hardware_config":{
					"hardware_profile_ref":{
						"name":"gpu-profile-v1",
						"namespace":"my-tenant"
					}
				}
			}]
		}`)

		cfg := &api.EvaluationJobConfig{}
		if err := serialization.Unmarshal(validate, ctx, body, cfg); err != nil {
			t.Fatalf("unmarshal request: %v", err)
		}
		if cfg.Benchmarks[0].HardwareConfig.HardwareProfileRef.Namespace != "my-tenant" {
			t.Fatalf("namespace = %q, want my-tenant", cfg.Benchmarks[0].HardwareConfig.HardwareProfileRef.Namespace)
		}

		job := &api.EvaluationJobResource{
			Resource:            api.EvaluationResource{Resource: api.Resource{ID: "job-1"}},
			EvaluationJobConfig: *cfg,
		}
		encoded, err := json.Marshal(job)
		if err != nil {
			t.Fatalf("marshal job: %v", err)
		}

		var decoded api.EvaluationJobResource
		if err := json.Unmarshal(encoded, &decoded); err != nil {
			t.Fatalf("unmarshal job: %v", err)
		}
		if decoded.Benchmarks[0].HardwareConfig.HardwareProfileRef.Namespace != "my-tenant" {
			t.Fatalf("response namespace = %q, want my-tenant", decoded.Benchmarks[0].HardwareConfig.HardwareProfileRef.Namespace)
		}
	})
}
