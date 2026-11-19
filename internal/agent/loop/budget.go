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

// Limits describes the resource limits for one generation run.
type Limits struct {
	// MaxIterations is the maximum number of model-and-tool loop iterations.
	MaxIterations int
	// MaxModelCalls is the maximum number of requests sent to the model.
	MaxModelCalls int
	// MaxToolCalls is the maximum number of individual tool invocations.
	MaxToolCalls int
	// MaxTotalTokens is the maximum number of input and output tokens accounted for across the run.
	MaxTotalTokens int64
	// MaxOutputTokens is the maximum number of tokens allowed in one model response.
	MaxOutputTokens int
	// MaxDuration is the maximum wall-clock time allowed for the run.
	MaxDuration time.Duration
}

// Budget is an immutable per-run snapshot. API callers select a preset; they do
// not construct or override these limits.
type Budget struct {
	Limits
}

// HardLimits cap server-configured presets so a bad configuration cannot
// remove resource bounds.
type HardLimits struct {
	Limits
}

// Presets maps the three public effort values to server-owned budgets.
type Presets map[Effort]Budget

var orderedEfforts = [...]Effort{EffortLow, EffortStandard, EffortHigh}

// DefaultHardLimits returns conservative service-wide ceilings. Deployments
// may choose lower ceilings, but should not raise these without review.
func DefaultHardLimits() HardLimits {
	return HardLimits{
		Limits: Limits{
			MaxIterations:   100,
			MaxModelCalls:   100,
			MaxToolCalls:    500,
			MaxTotalTokens:  1_000_000,
			MaxOutputTokens: 16_384,
			MaxDuration:     30 * time.Minute,
		},
	}
}

// DefaultPresets returns documented defaults that the service may override
// from trusted configuration before admitting a job.
func DefaultPresets() Presets {
	return Presets{
		EffortLow: {
			Limits: Limits{
				MaxIterations: 8, MaxModelCalls: 8, MaxToolCalls: 24,
				MaxTotalTokens: 32_768, MaxOutputTokens: 2_048, MaxDuration: 2 * time.Minute,
			},
		},
		EffortStandard: {
			Limits: Limits{
				MaxIterations: 20, MaxModelCalls: 20, MaxToolCalls: 60,
				MaxTotalTokens: 131_072, MaxOutputTokens: 4_096, MaxDuration: 2 * time.Minute,
			},
		},
		EffortHigh: {
			Limits: Limits{
				MaxIterations: 40, MaxModelCalls: 40, MaxToolCalls: 120,
				MaxTotalTokens: 524_288, MaxOutputTokens: 8_192, MaxDuration: 10 * time.Minute,
			},
		},
	}
}

// Validate requires all supported presets and validates each against the
// service's hard ceilings.
func (p Presets) Validate(caps HardLimits) error {
	if err := caps.validate(); err != nil {
		return fmt.Errorf("invalid hard limits: %w", err)
	}
	if caps.exceeds(DefaultHardLimits()) {
		return errors.New("configured hard limits exceed the shipped service ceilings")
	}
	for _, effort := range orderedEfforts {
		budget, ok := p[effort]
		if !ok {
			return fmt.Errorf("missing %q generation effort preset", effort)
		}
		if err := budget.validateAgainst(caps); err != nil {
			return fmt.Errorf("invalid %q generation effort preset: %w", effort, err)
		}
	}
	for i := 1; i < len(orderedEfforts); i++ {
		previous, current := p[orderedEfforts[i-1]], p[orderedEfforts[i]]
		if current.MaxIterations < previous.MaxIterations || current.MaxModelCalls < previous.MaxModelCalls || current.MaxToolCalls < previous.MaxToolCalls || current.MaxTotalTokens < previous.MaxTotalTokens || current.MaxOutputTokens < previous.MaxOutputTokens || current.MaxDuration < previous.MaxDuration {
			return fmt.Errorf("%q budget must be at least the preceding effort preset", orderedEfforts[i])
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

func (l HardLimits) validate() error {
	if l.MaxIterations <= 0 || l.MaxModelCalls <= 0 || l.MaxToolCalls <= 0 || l.MaxTotalTokens <= 0 || l.MaxOutputTokens <= 0 || l.MaxDuration <= 0 {
		return errors.New("all hard limits must be positive")
	}
	return nil
}

func (l HardLimits) exceeds(ceiling HardLimits) bool {
	return l.MaxIterations > ceiling.MaxIterations || l.MaxModelCalls > ceiling.MaxModelCalls || l.MaxToolCalls > ceiling.MaxToolCalls || l.MaxTotalTokens > ceiling.MaxTotalTokens || l.MaxOutputTokens > ceiling.MaxOutputTokens || l.MaxDuration > ceiling.MaxDuration
}

func (b Budget) validateAgainst(l HardLimits) error {
	if b.MaxIterations <= 0 || b.MaxModelCalls <= 0 || b.MaxToolCalls <= 0 || b.MaxTotalTokens <= 0 || b.MaxOutputTokens <= 0 || b.MaxDuration <= 0 {
		return errors.New("all budget values must be positive")
	}
	if b.MaxModelCalls < b.MaxIterations {
		return errors.New("max_model_calls must be at least max_iterations")
	}
	if int64(b.MaxOutputTokens) > b.MaxTotalTokens {
		return errors.New("max_output_tokens cannot exceed max_total_tokens")
	}
	if b.MaxIterations > l.MaxIterations || b.MaxModelCalls > l.MaxModelCalls || b.MaxToolCalls > l.MaxToolCalls || b.MaxTotalTokens > l.MaxTotalTokens || b.MaxOutputTokens > l.MaxOutputTokens || b.MaxDuration > l.MaxDuration {
		return errors.New("budget exceeds a service hard limit")
	}
	return nil
}
