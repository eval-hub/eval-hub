package loop

import (
	"testing"
	"time"
)

func TestDefaultPresetsValidateAndResolve(t *testing.T) {
	presets := DefaultPresets()
	caps := DefaultHardLimits()
	if err := presets.Validate(caps); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	standard, err := presets.Resolve("", caps)
	if err != nil {
		t.Fatalf("Resolve(default) error = %v", err)
	}
	if standard != presets[EffortStandard] {
		t.Fatalf("default resolved budget = %+v, want standard %+v", standard, presets[EffortStandard])
	}
	low, err := presets.Resolve(EffortLow, caps)
	if err != nil || low != presets[EffortLow] {
		t.Fatalf("Resolve(low) = %+v, %v", low, err)
	}
	high, err := presets.Resolve(EffortHigh, caps)
	if err != nil || high != presets[EffortHigh] {
		t.Fatalf("Resolve(high) = %+v, %v", high, err)
	}

	// A resolved value is a snapshot and does not change when its source map is
	// updated for later jobs.
	presets[EffortStandard] = Budget{Limits: Limits{MaxIterations: 1, MaxModelCalls: 1, MaxToolCalls: 1, MaxTotalTokens: 1, MaxOutputTokens: 1, MaxDuration: time.Second}}
	if standard.MaxIterations == presets[EffortStandard].MaxIterations {
		t.Fatal("resolved budget changed after preset update")
	}
}

func TestPresetsRejectMissingInvalidAndOverCapValues(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(Presets)
		wantErr bool
	}{
		{name: "valid defaults"},
		{name: "missing preset", mutate: func(p Presets) { delete(p, EffortHigh) }, wantErr: true},
		{name: "zero budget", mutate: func(p Presets) { b := p[EffortLow]; b.MaxModelCalls = 0; p[EffortLow] = b }, wantErr: true},
		{name: "iterations exceed calls", mutate: func(p Presets) { b := p[EffortLow]; b.MaxIterations = b.MaxModelCalls + 1; p[EffortLow] = b }, wantErr: true},
		{name: "exceeds hard cap", mutate: func(p Presets) {
			b := p[EffortLow]
			b.MaxTotalTokens = DefaultHardLimits().MaxTotalTokens + 1
			p[EffortLow] = b
		}, wantErr: true},
		{name: "output exceeds total", mutate: func(p Presets) { b := p[EffortLow]; b.MaxOutputTokens = int(b.MaxTotalTokens) + 1; p[EffortLow] = b }, wantErr: true},
		{name: "effort not monotonic", mutate: func(p Presets) {
			b := p[EffortHigh]
			b.MaxDuration = p[EffortStandard].MaxDuration - time.Second
			p[EffortHigh] = b
		}, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := DefaultPresets()
			if tc.mutate != nil {
				tc.mutate(p)
			}
			err := p.Validate(DefaultHardLimits())
			if (err != nil) != tc.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
	if _, err := DefaultPresets().Resolve("urgent", DefaultHardLimits()); err == nil {
		t.Fatal("Resolve(unknown effort) succeeded")
	}
}

func TestPresetValidationRejectsInvalidServiceCapsAndBudgetDeadline(t *testing.T) {
	if err := DefaultPresets().Validate(HardLimits{}); err == nil {
		t.Fatal("Validate() accepted zero service ceilings")
	}
	caps := DefaultHardLimits()
	caps.MaxDuration += time.Second
	if err := DefaultPresets().Validate(caps); err == nil {
		t.Fatal("Validate() accepted service ceilings above shipped limits")
	}

	presets := DefaultPresets()
	budget := presets[EffortLow]
	budget.MaxDuration = DefaultHardLimits().MaxDuration + time.Second
	presets[EffortLow] = budget
	if err := presets.Validate(DefaultHardLimits()); err == nil {
		t.Fatal("Validate() accepted a preset deadline beyond the hard cap")
	}
	if _, err := presets.Resolve(EffortLow, DefaultHardLimits()); err == nil {
		t.Fatal("Resolve() accepted invalid presets")
	}
}
