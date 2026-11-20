package config

import (
	"fmt"
	"time"

	"github.com/eval-hub/eval-hub/internal/agent/loop"
)

// ServiceGenerationConfig contains trusted deployment controls for collection-generation jobs.
// Generation remains disabled until a resolved runner image is supplied by deployment config.
type ServiceGenerationConfig struct {
	// RunnerImage is the exact EvalHub image reference used for generation worker Jobs.
	RunnerImage string `mapstructure:"runner_image,omitempty"`
	// Retention is the terminal generation-record lifetime; zero selects the 24-hour default.
	Retention time.Duration `mapstructure:"retention,omitempty"`
	// CleanupInterval controls how often each service replica runs a bounded retention sweep.
	CleanupInterval time.Duration `mapstructure:"cleanup_interval,omitempty"`
	// CleanupBatchSize bounds rows deleted by one retention sweep.
	CleanupBatchSize int `mapstructure:"cleanup_batch_size,omitempty"`
	// EffortBudgets partially configure limits for each generation_effort level.
	// Omitted fields retain library defaults; explicit zero values are rejected.
	EffortBudgets map[string]GenerationEffortBudget `mapstructure:"effort_budget,omitempty"`
	// ReasoningCapabilities explicitly opts exact upstream model URL/name pairs into provider reasoning controls.
	ReasoningCapabilities []GenerationReasoningCapability `mapstructure:"reasoning_capabilities,omitempty"`
}

// GenerationEffortBudget allows deployment configuration to set individual limits
// for one generation_effort level. Pointer fields distinguish an omitted limit from zero.
type GenerationEffortBudget struct {
	MaxIterations   *int           `mapstructure:"max_iterations,omitempty"`
	MaxModelCalls   *int           `mapstructure:"max_model_calls,omitempty"`
	MaxToolCalls    *int           `mapstructure:"max_tool_calls,omitempty"`
	MaxTotalTokens  *int64         `mapstructure:"max_total_tokens,omitempty"`
	MaxOutputTokens *int           `mapstructure:"max_output_tokens,omitempty"`
	MaxDuration     *time.Duration `mapstructure:"max_duration,omitempty"`
}

// GenerationReasoningCapability binds provider-specific reasoning values to one exact upstream endpoint.
type GenerationReasoningCapability struct {
	ModelURL  string            `mapstructure:"model_url,omitempty"`
	ModelName string            `mapstructure:"model_name,omitempty"`
	Efforts   map[string]string `mapstructure:"efforts,omitempty"`
}

const (
	DefaultGenerationRetention       = 24 * time.Hour
	DefaultGenerationCleanupInterval = time.Minute
	MinGenerationCleanupInterval     = time.Second
	MaxGenerationCleanupInterval     = 24 * time.Hour
	DefaultGenerationCleanupBatch    = 100
	MaxGenerationCleanupBatch        = 1000
)

// EffectiveEffortBudgets overlays explicit deployment values on the bounded library defaults.
func (c *ServiceGenerationConfig) EffectiveEffortBudgets() (loop.Presets, error) {
	presets := loop.DefaultPresets()
	if c == nil {
		return presets, presets.Validate(loop.DefaultHardLimits())
	}
	for name, overrides := range c.EffortBudgets {
		effort := loop.Effort(name)
		budget, ok := presets[effort]
		if !ok {
			return nil, fmt.Errorf("unknown generation effort preset %q", name)
		}
		if overrides.MaxIterations != nil {
			budget.MaxIterations = *overrides.MaxIterations
		}
		if overrides.MaxModelCalls != nil {
			budget.MaxModelCalls = *overrides.MaxModelCalls
		}
		if overrides.MaxToolCalls != nil {
			budget.MaxToolCalls = *overrides.MaxToolCalls
		}
		if overrides.MaxTotalTokens != nil {
			budget.MaxTotalTokens = *overrides.MaxTotalTokens
		}
		if overrides.MaxOutputTokens != nil {
			budget.MaxOutputTokens = *overrides.MaxOutputTokens
		}
		if overrides.MaxDuration != nil {
			budget.MaxDuration = *overrides.MaxDuration
		}
		presets[effort] = budget
	}
	if err := presets.Validate(loop.DefaultHardLimits()); err != nil {
		return nil, err
	}
	return presets, nil
}

// Validate rejects invalid retention settings, incomplete endpoint capability entries,
// and invalid effort presets before the service accepts requests.
func (c *ServiceGenerationConfig) Validate() error {
	if c == nil {
		return nil
	}
	if c.Retention < 0 {
		return fmt.Errorf("service.generation.retention must not be negative")
	}
	if c.CleanupInterval < 0 {
		return fmt.Errorf("service.generation.cleanup_interval must not be negative")
	}
	if c.CleanupInterval > 0 && c.CleanupInterval < MinGenerationCleanupInterval {
		return fmt.Errorf("service.generation.cleanup_interval must be at least %s", MinGenerationCleanupInterval)
	}
	if c.CleanupInterval > MaxGenerationCleanupInterval {
		return fmt.Errorf("service.generation.cleanup_interval must not exceed %s", MaxGenerationCleanupInterval)
	}
	if c.CleanupBatchSize < 0 {
		return fmt.Errorf("service.generation.cleanup_batch_size must not be negative")
	}
	if c.CleanupBatchSize > MaxGenerationCleanupBatch {
		return fmt.Errorf("service.generation.cleanup_batch_size must not exceed %d", MaxGenerationCleanupBatch)
	}
	if _, err := c.EffectiveEffortBudgets(); err != nil {
		return fmt.Errorf("service.generation.effort_budget: %w", err)
	}
	seen := make(map[string]struct{}, len(c.ReasoningCapabilities))
	for i, capability := range c.ReasoningCapabilities {
		if capability.ModelURL == "" || capability.ModelName == "" || len(capability.Efforts) == 0 {
			return fmt.Errorf("service.generation.reasoning_capabilities[%d] requires model_url, model_name, and efforts", i)
		}
		key := capability.ModelURL + "\x00" + capability.ModelName
		if _, ok := seen[key]; ok {
			return fmt.Errorf("duplicate generation reasoning capability for model %q", capability.ModelName)
		}
		seen[key] = struct{}{}
		for effort, value := range capability.Efforts {
			if effort != string(loop.EffortLow) && effort != string(loop.EffortStandard) && effort != string(loop.EffortHigh) {
				return fmt.Errorf("service.generation.reasoning_capabilities[%d] has unknown effort %q", i, effort)
			}
			if value == "" {
				return fmt.Errorf("service.generation.reasoning_capabilities[%d].efforts.%s must not be empty", i, effort)
			}
		}
	}
	return nil
}

// ResolveReasoningEffort returns an explicitly configured provider value only for an exact
// pre-sidecar URL and model-name match. Unknown or unmatched models omit the provider control.
func (c *ServiceGenerationConfig) ResolveReasoningEffort(modelURL, modelName, effort string) string {
	if c == nil {
		return ""
	}
	for _, capability := range c.ReasoningCapabilities {
		if capability.ModelURL == modelURL && capability.ModelName == modelName {
			return capability.Efforts[effort]
		}
	}
	return ""
}

// EffectiveRetention returns the configured terminal-record retention duration.
func (c *ServiceGenerationConfig) EffectiveRetention() time.Duration {
	if c == nil || c.Retention == 0 {
		return DefaultGenerationRetention
	}
	return c.Retention
}

// EffectiveCleanupInterval returns the configured bounded-sweep interval.
func (c *ServiceGenerationConfig) EffectiveCleanupInterval() time.Duration {
	if c == nil || c.CleanupInterval == 0 {
		return DefaultGenerationCleanupInterval
	}
	return c.CleanupInterval
}

// EffectiveCleanupBatchSize returns the maximum number of records removed in one sweep.
func (c *ServiceGenerationConfig) EffectiveCleanupBatchSize() int {
	if c == nil || c.CleanupBatchSize == 0 {
		return DefaultGenerationCleanupBatch
	}
	return c.CleanupBatchSize
}
