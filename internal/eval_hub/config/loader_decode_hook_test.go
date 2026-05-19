package config

import (
	"testing"
	"time"

	"github.com/go-viper/mapstructure/v2"
)

func TestConfigDecodeHookParsesDurationAndSlice(t *testing.T) {
	type sample struct {
		Wait time.Duration `mapstructure:"wait"`
		Tags []string      `mapstructure:"tags"`
	}

	var s sample
	decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		DecodeHook: configDecodeHook(),
		Result:     &s,
	})
	if err != nil {
		t.Fatalf("NewDecoder: %v", err)
	}
	if err := decoder.Decode(map[string]any{
		"wait": "30m",
		"tags": "a,b",
	}); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if s.Wait != 30*time.Minute {
		t.Errorf("wait = %v, want 30m", s.Wait)
	}
	if len(s.Tags) != 2 || s.Tags[0] != "a" || s.Tags[1] != "b" {
		t.Errorf("tags = %v, want [a b]", s.Tags)
	}
}
