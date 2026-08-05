package config

import (
	"crypto/tls"
	"time"
)

type MLFlowConfig struct {
	TrackingURI string        `mapstructure:"tracking_uri"`
	InternalURI string        `mapstructure:"internal_uri"`
	HTTPTimeout time.Duration `mapstructure:"http_timeout"`
	CACertPath  string        `mapstructure:"ca_cert_path"`
	Token       string        `mapstructure:"token"`
	TokenPath   string        `mapstructure:"token_path"`
	Workspace   string        `mapstructure:"workspace"`
	TLSConfig   *tls.Config   // not serialized
}

// ConnectionURI returns the URI to use for actual MLflow connections.
// Prefers InternalURI (in-cluster service URL) when set, falling back to TrackingURI.
func (c *MLFlowConfig) ConnectionURI() string {
	if c.InternalURI != "" {
		return c.InternalURI
	}
	return c.TrackingURI
}
