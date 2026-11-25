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
	t.Setenv("EVALHUB_RESOURCE_JANITOR_INTERVAL", "90m")
	t.Setenv("EVALHUB_GENERATION_STANDARD_MAX_ITERATIONS", "25")
	t.Setenv("EVALHUB_GENERATION_STANDARD_MAX_MODEL_CALLS", "25")
	t.Setenv("EVALHUB_GENERATION_STANDARD_MAX_DURATION", "3m")
	loaded, err := config.LoadConfig(slog.New(slog.DiscardHandler), "test", "", "", "", "../../../config")
	if err != nil {
		t.Fatalf("LoadConfig(): %v", err)
	}
	if got := loaded.Generation.RunnerImage; got != "registry.example/evalhub:v1" {
		t.Fatalf("generation runner image = %q", got)
	}
	if got := loaded.ResourceJanitor.EffectiveInterval(); got != 90*time.Minute {
		t.Fatalf("resource janitor interval = %s, want 90m", got)
	}
	presets, err := loaded.Generation.EffectiveEffortBudgets()
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

func TestGenerationConfigEffectiveEffortBudgets(t *testing.T) {
	standardIterations := 15
	standardCalls := loop.DefaultPresets()[loop.EffortStandard].MaxModelCalls
	standardToolCalls := loop.DefaultPresets()[loop.EffortStandard].MaxToolCalls
	standardTokens := loop.DefaultPresets()[loop.EffortStandard].MaxTotalTokens
	standardOutput := loop.DefaultPresets()[loop.EffortStandard].MaxOutputTokens
	standardDuration := 3 * time.Minute
	cfg := &config.GenerationConfig{EffortBudgets: map[string]config.GenerationEffortBudget{
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
	var nilConfig *config.GenerationConfig
	if _, err := nilConfig.EffectiveEffortBudgets(); err != nil {
		t.Fatalf("nil config did not use bounded defaults: %v", err)
	}
}

func TestGenerationConfigRejectsInvalidPresets(t *testing.T) {
	zero := 0
	tooLong := 31 * time.Minute
	badOrdering := 7
	tests := []struct {
		name string
		cfg  *config.GenerationConfig
	}{
		{name: "explicit zero", cfg: &config.GenerationConfig{EffortBudgets: map[string]config.GenerationEffortBudget{"low": {MaxIterations: &zero}}}},
		{name: "above hard limit", cfg: &config.GenerationConfig{EffortBudgets: map[string]config.GenerationEffortBudget{"high": {MaxDuration: &tooLong}}}},
		{name: "effort ordering", cfg: &config.GenerationConfig{EffortBudgets: map[string]config.GenerationEffortBudget{"standard": {MaxIterations: &badOrdering}}}},
		{name: "unknown effort", cfg: &config.GenerationConfig{EffortBudgets: map[string]config.GenerationEffortBudget{"urgent": {}}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.cfg.Validate(); err == nil {
				t.Fatal("Validate() succeeded for invalid preset configuration")
			}
		})
	}
}

func TestGenerationConfigRetentionDefaultsAndValidation(t *testing.T) {
	var nilConfig *config.GenerationConfig
	if got := nilConfig.EffectiveRetention(); got != config.DefaultGenerationRetention {
		t.Fatalf("nil retention = %s, want %s", got, config.DefaultGenerationRetention)
	}
	configured := &config.GenerationConfig{Retention: 2 * time.Hour}
	if got := configured.EffectiveRetention(); got != 2*time.Hour {
		t.Fatalf("configured retention = %s", got)
	}
	if err := (&config.GenerationConfig{Retention: -time.Second}).Validate(); err == nil {
		t.Fatal("Validate() accepted negative retention")
	}
}

func TestResourceJanitorConfigDefaultsAndValidation(t *testing.T) {
	var omitted *config.ResourceJanitorConfig
	if err := omitted.Validate(); err != nil || omitted.EffectiveInterval() != 24*time.Hour {
		t.Fatalf("omitted config = (%s, %v), want default interval", omitted.EffectiveInterval(), err)
	}
	if got := (&config.ResourceJanitorConfig{}).EffectiveInterval(); got != 24*time.Hour {
		t.Fatalf("zero interval = %s, want 24h", got)
	}
	for _, interval := range []time.Duration{0, time.Minute, 90 * time.Minute, 24 * time.Hour} {
		if err := (&config.ResourceJanitorConfig{Interval: interval}).Validate(); err != nil {
			t.Errorf("valid interval %s rejected: %v", interval, err)
		}
	}
	for _, interval := range []time.Duration{-time.Minute, time.Second, 90 * time.Second, 25 * time.Hour} {
		if err := (&config.ResourceJanitorConfig{Interval: interval}).Validate(); err == nil {
			t.Errorf("invalid interval %s accepted", interval)
		}
	}
}

func TestGenerationConfigResolveReasoningEffortExactMatch(t *testing.T) {
	cfg := &config.GenerationConfig{ReasoningCapabilities: []config.GenerationReasoningCapability{{
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

func TestGenerationConfigRejectsInvalidReasoningCapabilities(t *testing.T) {
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
			if err := (&config.GenerationConfig{ReasoningCapabilities: tc.caps}).Validate(); err == nil {
				t.Fatal("Validate() succeeded")
			}
		})
	}
}
