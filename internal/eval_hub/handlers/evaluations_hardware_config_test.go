package handlers_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/executioncontext"
	"github.com/eval-hub/eval-hub/internal/eval_hub/serialization"
	"github.com/eval-hub/eval-hub/internal/testhelpers"
	"github.com/eval-hub/eval-hub/pkg/api"
)

func TestEvaluationJobConfigHardwareConfigRoundTrip(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	validate := testhelpers.NewValidator(t)
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-hwp", logger, "test-user", "test-tenant")

	body := []byte(`{
		"name":"test-job",
		"model":{"url":"http://test.com","name":"model"},
		"benchmarks":[{
			"id":"b1",
			"provider_id":"provider-1",
			"hardware_config":{
				"hardware_profile_name":"cpu-optimized-profile"
			}
		}]
	}`)

	cfg := &api.EvaluationJobConfig{}
	if err := serialization.Unmarshal(validate, ctx, body, cfg); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	if cfg.Benchmarks[0].HardwareConfig.HardwareProfileName != "cpu-optimized-profile" {
		t.Fatalf("name = %q, want cpu-optimized-profile", cfg.Benchmarks[0].HardwareConfig.HardwareProfileName)
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
	if hardwareConfig["hardware_profile_name"] != "cpu-optimized-profile" {
		t.Fatalf("hardware_profile_name = %v, want cpu-optimized-profile", hardwareConfig["hardware_profile_name"])
	}
	for _, field := range []string{"queue", "cpu", "memory", "gpu"} {
		if _, ok := hardwareConfig[field]; ok {
			t.Fatalf("%s should not be present in profile-mode response, got: %s", field, string(encoded))
		}
	}
}

func TestEvaluationJobConfigHardwareConfigDirectRoundTrip(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	validate := testhelpers.NewValidator(t)
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-hwp-direct", logger, "test-user", "test-tenant")

	body := []byte(`{
		"name":"test-job",
		"model":{"url":"http://test.com","name":"model"},
		"benchmarks":[{
			"id":"b1",
			"provider_id":"provider-1",
			"hardware_config":{
				"queue":{"kind":"kueue","name":"my-queue"},
				"cpu":{"request":"1","limit":"2"},
				"memory":{"request":"1Gi","limit":"2Gi"},
				"gpu":{"name":"nvidia.com/gpu","count":1}
			}
		}]
	}`)

	cfg := &api.EvaluationJobConfig{}
	if err := serialization.Unmarshal(validate, ctx, body, cfg); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	hw := cfg.Benchmarks[0].HardwareConfig
	if hw.HardwareProfileName != "" {
		t.Fatalf("hardware_profile_name = %q, want empty", hw.HardwareProfileName)
	}
	if hw.Queue == nil || hw.Queue.Name != "my-queue" || hw.Queue.Kind != "kueue" {
		t.Fatalf("unexpected queue: %#v", hw.Queue)
	}
	if hw.CPU == nil || hw.CPU.Request != "1" || hw.CPU.Limit != "2" {
		t.Fatalf("unexpected cpu: %#v", hw.CPU)
	}
	if hw.Memory == nil || hw.Memory.Request != "1Gi" || hw.Memory.Limit != "2Gi" {
		t.Fatalf("unexpected memory: %#v", hw.Memory)
	}
	if hw.GPU == nil || hw.GPU.Name != "nvidia.com/gpu" || hw.GPU.Count != 1 {
		t.Fatalf("unexpected gpu: %#v", hw.GPU)
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
	benchmark := response["benchmarks"].([]any)[0].(map[string]any)
	hardwareConfig := benchmark["hardware_config"].(map[string]any)
	if _, ok := hardwareConfig["hardware_profile_name"]; ok {
		t.Fatalf("hardware_profile_name should not be present in direct-mode response, got: %s", string(encoded))
	}
}

func TestEvaluationJobConfigHardwareConfigRejectsProfileWithDirectFields(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	validate := testhelpers.NewValidator(t)
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-hwp-exclusive", logger, "test-user", "test-tenant")

	body := []byte(`{
		"name":"test-job",
		"model":{"url":"http://test.com","name":"model"},
		"benchmarks":[{
			"id":"b1",
			"provider_id":"provider-1",
			"hardware_config":{
				"hardware_profile_name":"my-hw-spec",
				"cpu":{"request":"1","limit":"2"}
			}
		}]
	}`)

	cfg := &api.EvaluationJobConfig{}
	err := serialization.Unmarshal(validate, ctx, body, cfg)
	if err == nil {
		t.Fatal("expected validation error when hardware_profile_name is combined with direct fields")
	}
	if got := err.Error(); !strings.Contains(got, "hardware_config") || !strings.Contains(got, "hardware_profile_name") {
		t.Fatalf("error = %q, want hardware_config exclusivity message", got)
	}
}

func TestEvaluationJobConfigEvaluationLevelHardwareConfigRoundTrip(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	validate := testhelpers.NewValidator(t)
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-eval-hw", logger, "test-user", "test-tenant")

	body := []byte(`{
		"name":"test-job",
		"model":{"url":"http://test.com","name":"model"},
		"benchmarks":[{"id":"b1","provider_id":"provider-1"}],
		"hardware_config":{
			"queue":{"kind":"kueue","name":"fallback-queue"},
			"cpu":{"request":"1","limit":"2"}
		},
		"queue":{"kind":"kueue","name":"legacy-queue"}
	}`)

	cfg := &api.EvaluationJobConfig{}
	if err := serialization.Unmarshal(validate, ctx, body, cfg); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	if cfg.HardwareConfig == nil || cfg.HardwareConfig.Queue == nil || cfg.HardwareConfig.Queue.Name != "fallback-queue" {
		t.Fatalf("unexpected evaluation.hardware_config: %#v", cfg.HardwareConfig)
	}
	if cfg.Queue == nil || cfg.Queue.Name != "legacy-queue" {
		t.Fatalf("unexpected evaluation.queue: %#v", cfg.Queue)
	}
	if cfg.Benchmarks[0].HardwareConfig != nil {
		t.Fatal("expected benchmark hardware_config to stay nil")
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
	hw, ok := response["hardware_config"].(map[string]any)
	if !ok {
		t.Fatalf("hardware_config missing in response: %s", string(encoded))
	}
	queue, ok := hw["queue"].(map[string]any)
	if !ok || queue["name"] != "fallback-queue" {
		t.Fatalf("unexpected hardware_config.queue in response: %s", string(encoded))
	}
	topQueue, ok := response["queue"].(map[string]any)
	if !ok || topQueue["name"] != "legacy-queue" {
		t.Fatalf("unexpected evaluation.queue in response: %s", string(encoded))
	}
}

func TestEvaluationJobConfigEvaluationLevelHardwareConfigRejectsProfileWithDirectFields(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	validate := testhelpers.NewValidator(t)
	ctx := executioncontext.NewExecutionContext(context.Background(), "req-eval-hw-exclusive", logger, "test-user", "test-tenant")

	body := []byte(`{
		"name":"test-job",
		"model":{"url":"http://test.com","name":"model"},
		"benchmarks":[{"id":"b1","provider_id":"provider-1"}],
		"hardware_config":{
			"hardware_profile_name":"my-hw-spec",
			"cpu":{"request":"1","limit":"2"}
		}
	}`)

	cfg := &api.EvaluationJobConfig{}
	err := serialization.Unmarshal(validate, ctx, body, cfg)
	if err == nil {
		t.Fatal("expected validation error for evaluation.hardware_config exclusivity")
	}
	if got := err.Error(); !strings.Contains(got, "hardware_config") || !strings.Contains(got, "hardware_profile_name") {
		t.Fatalf("error = %q, want hardware_config exclusivity message", got)
	}
}

func TestEffectiveHardwareConfig(t *testing.T) {
	t.Parallel()

	benchHW := &api.BenchmarkHardwareConfig{Queue: &api.QueueConfig{Name: "bench"}}
	evalHW := &api.BenchmarkHardwareConfig{Queue: &api.QueueConfig{Name: "eval"}}

	if got := api.EffectiveHardwareConfig(nil, nil); got != nil {
		t.Fatalf("got %#v, want nil", got)
	}
	if got := api.EffectiveHardwareConfig(&api.EvaluationBenchmarkConfig{}, &api.EvaluationJobConfig{HardwareConfig: evalHW}); got != evalHW {
		t.Fatalf("got %#v, want evaluation fallback", got)
	}
	if got := api.EffectiveHardwareConfig(&api.EvaluationBenchmarkConfig{HardwareConfig: benchHW}, &api.EvaluationJobConfig{HardwareConfig: evalHW}); got != benchHW {
		t.Fatalf("got %#v, want benchmark config", got)
	}
}
