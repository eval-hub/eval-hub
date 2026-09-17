package config

import (
	"crypto/tls"
	"strings"
	"time"
)

type MLFlowConfig struct {
	TrackingURI string        `mapstructure:"tracking_uri"`
	HTTPTimeout time.Duration `mapstructure:"http_timeout"`
	CACertPath  string        `mapstructure:"ca_cert_path"`
	Token       string        `mapstructure:"token"`
	TokenPath   string        `mapstructure:"token_path"`
	Workspace   string        `mapstructure:"workspace"`
	TLSConfig   *tls.Config   // not serialized
}

// EffectiveTrackingURI returns the configured MLflow tracking URI without
// surrounding whitespace. A nil config represents an unset URI.
func (c *MLFlowConfig) EffectiveTrackingURI() string {
	if c == nil {
		return ""
	}
	return strings.TrimSpace(c.TrackingURI)
}
