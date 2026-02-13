package config

import (
	"crypto/tls"
	"time"
)

type MLFlowConfig struct {
	TrackingURI        string        `mapstructure:"tracking_uri"`
	HTTPTimeout        time.Duration `mapstructure:"http_timeout"`
	CACertPath         string        `mapstructure:"ca_cert_path"`
	InsecureSkipVerify bool          `mapstructure:"insecure_skip_verify"`
	TokenPath          string        `mapstructure:"token_path"`
	Workspace          string        `mapstructure:"workspace"`
	TLSConfig          *tls.Config   // not serialized
}
