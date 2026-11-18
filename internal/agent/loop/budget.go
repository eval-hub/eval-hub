package loop

import (
	"errors"
	"fmt"
	"time"
)

// Effort selects a server-owned generation budget preset.
type Effort string

const (
	EffortLow      Effort = "low"
	EffortStandard Effort = "standard"
	EffortHigh     Effort = "high"
)

// Budget is an immutable per-run snapshot. API callers select a preset; they do
// not construct or override these limits.
type Budget struct {
	MaxIterations   int
	MaxModelCalls   int
	MaxToolCalls    int
	MaxTotalTokens  int64
	MaxOutputTokens int
	Deadline        time.Duration
}

// HardLimits cap server-configured presets so a bad configuration cannot
// remove resource bounds.
type HardLimits struct {
	MaxIterations   int
	MaxModelCalls   int
	MaxToolCalls    int
	MaxTotalTokens  int64
	MaxOutputTokens int
	MaxDeadline     time.Duration
}

// Presets maps the three public effort values to server-owned budgets.
type Presets map[Effort]Budget

// DefaultHardLimits returns conservative service-wide ceilings. Deployments
// may choose lower ceilings, but should not raise these without review.
func DefaultHardLimits() HardLimits {
	return HardLimits{
		MaxIterations:   100,
		MaxModelCalls:   100,
		MaxToolCalls:    500,
		MaxTotalTokens:  1_000_000,
		MaxOutputTokens: 16_384,
		MaxDeadline:     30 * time.Minute,
	}
}

// DefaultPresets returns documented defaults that the service may override
// from trusted configuration before admitting a job.
func DefaultPresets() Presets {
	return Presets{
		EffortLow: {
			MaxIterations: 8, MaxModelCalls: 8, MaxToolCalls: 24,
			MaxTotalTokens: 32_768, MaxOutputTokens: 2_048, Deadline: 2 * time.Minute,
		},
		EffortStandard: {
			MaxIterations: 20, MaxModelCalls: 20, MaxToolCalls: 60,
			MaxTotalTokens: 131_072, MaxOutputTokens: 4_096, Deadline: 2 * time.Minute,
		},
		EffortHigh: {
			MaxIterations: 40, MaxModelCalls: 40, MaxToolCalls: 120,
			MaxTotalTokens: 524_288, MaxOutputTokens: 8_192, Deadline: 10 * time.Minute,
		},
	}
}

// Validate requires all supported presets and validates each against the
// service's hard ceilings.
func (p Presets) Validate(caps HardLimits) error {
	if err := validateHardLimits(caps); err != nil {
		return fmt.Errorf("invalid hard limits: %w", err)
	}
	if exceedsHardLimits(caps, DefaultHardLimits()) {
		return errors.New("configured hard limits exceed the shipped service ceilings")
	}
	ordered := []Effort{EffortLow, EffortStandard, EffortHigh}
	for _, effort := range ordered {
		budget, ok := p[effort]
		if !ok {
			return fmt.Errorf("missing %q generation effort preset", effort)
		}
		if err := validateBudget(budget, caps); err != nil {
			return fmt.Errorf("invalid %q generation effort preset: %w", effort, err)
		}
	}
	for i := 1; i < len(ordered); i++ {
		previous, current := p[ordered[i-1]], p[ordered[i]]
		if current.MaxIterations < previous.MaxIterations || current.MaxModelCalls < previous.MaxModelCalls || current.MaxToolCalls < previous.MaxToolCalls || current.MaxTotalTokens < previous.MaxTotalTokens || current.MaxOutputTokens < previous.MaxOutputTokens || current.Deadline < previous.Deadline {
			return fmt.Errorf("%q budget must be at least the preceding effort preset", ordered[i])
		}
	}
	return nil
}

// Resolve returns a value copy of the selected preset. An empty effort selects
// standard, so later configuration edits cannot mutate an admitted run.
func (p Presets) Resolve(effort Effort, caps HardLimits) (Budget, error) {
	if effort == "" {
		effort = EffortStandard
	}
	if err := p.Validate(caps); err != nil {
		return Budget{}, err
	}
	budget, ok := p[effort]
	if !ok {
		return Budget{}, fmt.Errorf("unsupported generation effort %q", effort)
	}
	return budget, nil
}

func validateHardLimits(c HardLimits) error {
	if c.MaxIterations <= 0 || c.MaxModelCalls <= 0 || c.MaxToolCalls <= 0 || c.MaxTotalTokens <= 0 || c.MaxOutputTokens <= 0 || c.MaxDeadline <= 0 {
		return errors.New("all hard limits must be positive")
	}
	return nil
}

func exceedsHardLimits(value, ceiling HardLimits) bool {
	return value.MaxIterations > ceiling.MaxIterations || value.MaxModelCalls > ceiling.MaxModelCalls || value.MaxToolCalls > ceiling.MaxToolCalls || value.MaxTotalTokens > ceiling.MaxTotalTokens || value.MaxOutputTokens > ceiling.MaxOutputTokens || value.MaxDeadline > ceiling.MaxDeadline
}

func validateBudget(b Budget, c HardLimits) error {
	if b.MaxIterations <= 0 || b.MaxModelCalls <= 0 || b.MaxToolCalls <= 0 || b.MaxTotalTokens <= 0 || b.MaxOutputTokens <= 0 || b.Deadline <= 0 {
		return errors.New("all budget values must be positive")
	}
	if b.MaxModelCalls < b.MaxIterations {
		return errors.New("max_model_calls must be at least max_iterations")
	}
	if int64(b.MaxOutputTokens) > b.MaxTotalTokens {
		return errors.New("max_output_tokens cannot exceed max_total_tokens")
	}
	if b.MaxIterations > c.MaxIterations || b.MaxModelCalls > c.MaxModelCalls || b.MaxToolCalls > c.MaxToolCalls || b.MaxTotalTokens > c.MaxTotalTokens || b.MaxOutputTokens > c.MaxOutputTokens || b.Deadline > c.MaxDeadline {
		return errors.New("budget exceeds a service hard limit")
	}
	return nil
}
