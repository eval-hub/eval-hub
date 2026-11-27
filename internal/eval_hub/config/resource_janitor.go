package config

import (
	"errors"
	"time"
)

// ResourceJanitorConfig controls the cadence of background maintenance.
type ResourceJanitorConfig struct {
	Interval time.Duration `mapstructure:"interval,omitempty"`
}

const DefaultResourceJanitorInterval = 24 * time.Hour

func (c *ResourceJanitorConfig) Validate() error {
	if c == nil || c.Interval == 0 {
		return nil
	}
	if c.Interval < time.Minute || c.Interval > 24*time.Hour || c.Interval%time.Minute != 0 {
		return errors.New("resource_janitor.interval must be a whole number of minutes between 1m and 24h")
	}
	return nil
}

func (c *ResourceJanitorConfig) EffectiveInterval() time.Duration {
	if c == nil || c.Interval == 0 {
		return DefaultResourceJanitorInterval
	}
	return c.Interval
}
