package config_test

import (
	"crypto/tls"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
)

func TestMLFlowConfigIsInsecureSkipVerify(t *testing.T) {
	tests := []struct {
		name string
		cfg  *config.MLFlowConfig
		want bool
	}{
		{
			name: "nil TLSConfig",
			cfg:  &config.MLFlowConfig{},
			want: false,
		},
		{
			name: "TLSConfig with InsecureSkipVerify false",
			cfg:  &config.MLFlowConfig{TLSConfig: &tls.Config{}},
			want: false,
		},
		{
			name: "TLSConfig with InsecureSkipVerify true",
			cfg:  &config.MLFlowConfig{TLSConfig: &tls.Config{InsecureSkipVerify: true}}, //nolint:gosec
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.IsInsecureSkipVerify(); got != tt.want {
				t.Errorf("IsInsecureSkipVerify() = %v, want %v", got, tt.want)
			}
		})
	}
}
