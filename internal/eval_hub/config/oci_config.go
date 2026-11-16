package config

import "time"

const DefaultOCIPushTimeout = 5 * time.Minute

func (c *SidecarOCIConfig) EffectivePushTimeout() time.Duration {
	if c == nil || c.PushTimeout <= 0 {
		return DefaultOCIPushTimeout
	}
	return c.PushTimeout
}

// EffectiveOCIPushTimeout reads the API service's publication deadline from the
// OCI configuration shared with the sidecar registry proxy.
func (c *Config) EffectiveOCIPushTimeout() time.Duration {
	if c == nil || c.Sidecar == nil {
		return DefaultOCIPushTimeout
	}
	return c.Sidecar.OCI.EffectivePushTimeout()
}
