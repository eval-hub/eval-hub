package config_test

import (
	"log/slog"
	"testing"
	"time"

	"github.com/eval-hub/eval-hub/internal/agent/loop"
	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
)

func TestLoadConfigMapsGenerationBudgetEnvironmentVariables(t *testing.T) {
	t.Setenv("CONFIG_PATH", "")
	t.Setenv("EVALHUB_COLLECTION_GENERATION_RUNNER_IMAGE", "registry.example/evalhub:v1")
	t.Setenv("EVALHUB_GENERATION_STANDARD_MAX_ITERATIONS", "25")
	t.Setenv("EVALHUB_GENERATION_STANDARD_MAX_MODEL_CALLS", "25")
	t.Setenv("EVALHUB_GENERATION_STANDARD_MAX_DURATION", "3m")
	loaded, err := config.LoadConfig(slog.New(slog.DiscardHandler), "test", "", "", "", "../../../config")
	if err != nil {
		t.Fatalf("LoadConfig(): %v", err)
	}
	if got := loaded.Service.Generation.RunnerImage; got != "registry.example/evalhub:v1" {
		t.Fatalf("generation runner image = %q", got)
	}
	presets, err := loaded.Service.Generation.EffectiveEffortBudgets()
	if err != nil {
		t.Fatalf("EffectiveEffortBudgets(): %v", err)
	}
	if got := presets[loop.EffortStandard].MaxIterations; got != 25 {
		t.Fatalf("standard max iterations = %d, want 25", got)
	}
	if got := presets[loop.EffortStandard].MaxDuration; got != 3*time.Minute {
		t.Fatalf("standard max duration = %s, want 3m", got)
	}
}

func TestServiceGenerationConfigEffectiveEffortBudgets(t *testing.T) {
	standardIterations := 15
	standardCalls := loop.DefaultPresets()[loop.EffortStandard].MaxModelCalls
	standardToolCalls := loop.DefaultPresets()[loop.EffortStandard].MaxToolCalls
	standardTokens := loop.DefaultPresets()[loop.EffortStandard].MaxTotalTokens
	standardOutput := loop.DefaultPresets()[loop.EffortStandard].MaxOutputTokens
	standardDuration := 3 * time.Minute
	cfg := &config.ServiceGenerationConfig{EffortBudgets: map[string]config.GenerationEffortBudget{
		"standard": {
			MaxIterations: &standardIterations, MaxModelCalls: &standardCalls,
			MaxToolCalls: &standardToolCalls, MaxTotalTokens: &standardTokens,
			MaxOutputTokens: &standardOutput, MaxDuration: &standardDuration,
		},
	}}

	presets, err := cfg.EffectiveEffortBudgets()
	if err != nil {
		t.Fatalf("EffectiveEffortBudgets() error = %v", err)
	}
	if got := presets[loop.EffortStandard].MaxIterations; got != standardIterations {
		t.Fatalf("standard max iterations = %d, want %d", got, standardIterations)
	}
	if got := presets[loop.EffortStandard].MaxDuration; got != standardDuration {
		t.Fatalf("standard max duration = %s, want %s", got, standardDuration)
	}
	if got := presets[loop.EffortLow].MaxIterations; got != loop.DefaultPresets()[loop.EffortLow].MaxIterations {
		t.Fatalf("omitted low preset was not inherited: got %d", got)
	}
	var nilConfig *config.ServiceGenerationConfig
	if _, err := nilConfig.EffectiveEffortBudgets(); err != nil {
		t.Fatalf("nil config did not use bounded defaults: %v", err)
	}
}

func TestServiceGenerationConfigRejectsInvalidPresets(t *testing.T) {
	zero := 0
	tooLong := 31 * time.Minute
	badOrdering := 7
	tests := []struct {
		name string
		cfg  *config.ServiceGenerationConfig
	}{
		{name: "explicit zero", cfg: &config.ServiceGenerationConfig{EffortBudgets: map[string]config.GenerationEffortBudget{"low": {MaxIterations: &zero}}}},
		{name: "above hard limit", cfg: &config.ServiceGenerationConfig{EffortBudgets: map[string]config.GenerationEffortBudget{"high": {MaxDuration: &tooLong}}}},
		{name: "effort ordering", cfg: &config.ServiceGenerationConfig{EffortBudgets: map[string]config.GenerationEffortBudget{"standard": {MaxIterations: &badOrdering}}}},
		{name: "unknown effort", cfg: &config.ServiceGenerationConfig{EffortBudgets: map[string]config.GenerationEffortBudget{"urgent": {}}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.cfg.Validate(); err == nil {
				t.Fatal("Validate() succeeded for invalid preset configuration")
			}
		})
	}
}

func TestServiceGenerationConfigRetentionDefaultsAndValidation(t *testing.T) {
	var nilConfig *config.ServiceGenerationConfig
	if got := nilConfig.EffectiveRetention(); got != config.DefaultGenerationRetention {
		t.Fatalf("nil retention = %s, want %s", got, config.DefaultGenerationRetention)
	}
	if got := (&config.ServiceGenerationConfig{}).EffectiveCleanupInterval(); got != config.DefaultGenerationCleanupInterval {
		t.Fatalf("default cleanup interval = %s", got)
	}
	if got := (&config.ServiceGenerationConfig{}).EffectiveCleanupBatchSize(); got != config.DefaultGenerationCleanupBatch {
		t.Fatalf("default cleanup batch = %d", got)
	}
	if got := nilConfig.EffectiveCleanupInterval(); got != config.DefaultGenerationCleanupInterval {
		t.Fatalf("nil cleanup interval = %s", got)
	}
	if got := nilConfig.EffectiveCleanupBatchSize(); got != config.DefaultGenerationCleanupBatch {
		t.Fatalf("nil cleanup batch = %d", got)
	}
	configured := &config.ServiceGenerationConfig{Retention: 2 * time.Hour, CleanupInterval: 2 * time.Minute, CleanupBatchSize: 42}
	if got := configured.EffectiveRetention(); got != 2*time.Hour {
		t.Fatalf("configured retention = %s", got)
	}
	if got := configured.EffectiveCleanupInterval(); got != 2*time.Minute {
		t.Fatalf("configured cleanup interval = %s", got)
	}
	if got := configured.EffectiveCleanupBatchSize(); got != 42 {
		t.Fatalf("configured cleanup batch = %d", got)
	}
	for _, tc := range []struct {
		name string
		cfg  config.ServiceGenerationConfig
	}{
		{name: "negative retention", cfg: config.ServiceGenerationConfig{Retention: -time.Second}},
		{name: "negative interval", cfg: config.ServiceGenerationConfig{CleanupInterval: -time.Second}},
		{name: "interval below minimum", cfg: config.ServiceGenerationConfig{CleanupInterval: time.Millisecond}},
		{name: "interval above maximum", cfg: config.ServiceGenerationConfig{CleanupInterval: config.MaxGenerationCleanupInterval + time.Second}},
		{name: "negative batch", cfg: config.ServiceGenerationConfig{CleanupBatchSize: -1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.cfg.Validate(); err == nil {
				t.Fatal("Validate() succeeded")
			}
		})
	}
	if err := (&config.ServiceGenerationConfig{CleanupInterval: config.MinGenerationCleanupInterval}).Validate(); err != nil {
		t.Fatalf("custom cleanup interval should use the default batch size: %v", err)
	}
}

func TestServiceGenerationConfigResolveReasoningEffortExactMatch(t *testing.T) {
	cfg := &config.ServiceGenerationConfig{ReasoningCapabilities: []config.GenerationReasoningCapability{{
		ModelURL: "https://model.example/v1", ModelName: "reasoner",
		Efforts: map[string]string{"low": "minimal", "standard": "medium", "high": "high"},
	}}}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate(): %v", err)
	}
	if got := cfg.ResolveReasoningEffort("https://model.example/v1", "reasoner", "high"); got != "high" {
		t.Fatalf("exact match effort = %q", got)
	}
	for _, tc := range []struct{ url, name, effort string }{
		{"https://model.example/v1/", "reasoner", "high"},
		{"https://other.example/v1", "reasoner", "high"},
		{"https://model.example/v1", "other", "high"},
		{"https://model.example/v1", "reasoner", "unknown"},
	} {
		if got := cfg.ResolveReasoningEffort(tc.url, tc.name, tc.effort); got != "" {
			t.Fatalf("unmatched model/effort resolved to %q", got)
		}
	}
}

func TestServiceGenerationConfigRejectsInvalidReasoningCapabilities(t *testing.T) {
	tests := []struct {
		name string
		caps []config.GenerationReasoningCapability
	}{
		{name: "incomplete", caps: []config.GenerationReasoningCapability{{ModelName: "model"}}},
		{name: "unknown effort", caps: []config.GenerationReasoningCapability{{ModelURL: "https://m/v1", ModelName: "m", Efforts: map[string]string{"urgent": "x"}}}},
		{name: "empty provider value", caps: []config.GenerationReasoningCapability{{ModelURL: "https://m/v1", ModelName: "m", Efforts: map[string]string{"low": ""}}}},
		{name: "duplicate model", caps: []config.GenerationReasoningCapability{
			{ModelURL: "https://m/v1", ModelName: "m", Efforts: map[string]string{"low": "low"}},
			{ModelURL: "https://m/v1", ModelName: "m", Efforts: map[string]string{"high": "high"}},
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := (&config.ServiceGenerationConfig{ReasoningCapabilities: tc.caps}).Validate(); err == nil {
				t.Fatal("Validate() succeeded")
			}
		})
	}
}
