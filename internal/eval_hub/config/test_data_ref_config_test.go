package config

import (
	"testing"
	"time"
)

func TestTestDataRefServiceConfigEffectiveDownloadTimeout(t *testing.T) {
	if got := (&TestDataRefServiceConfig{}).EffectiveDownloadTimeout(); got != DefaultTestDataRefDownloadTimeout {
		t.Fatalf("got %v, want default %v", got, DefaultTestDataRefDownloadTimeout)
	}
	if got := (&TestDataRefServiceConfig{DownloadTimeout: 20 * time.Minute}).EffectiveDownloadTimeout(); got != 20*time.Minute {
		t.Fatalf("got %v, want 20m", got)
	}
}

func TestTestDataRefServiceConfigEffectiveHFEndpoint(t *testing.T) {
	if got := (&TestDataRefServiceConfig{}).EffectiveHFEndpoint(); got != DefaultHFHubEndpoint {
		t.Fatalf("got %q, want default", got)
	}
	if got := (&TestDataRefServiceConfig{HF: &HFTestDataRefServiceConfig{Endpoint: "https://hf.example"}}).EffectiveHFEndpoint(); got != "https://hf.example" {
		t.Fatalf("got %q", got)
	}
}

func TestTestDataRefServiceConfigValidate(t *testing.T) {
	if err := (&TestDataRefServiceConfig{DownloadTimeout: -1}).Validate(); err == nil {
		t.Fatal("expected negative download timeout to be rejected")
	}
	if err := (&TestDataRefServiceConfig{
		HF: &HFTestDataRefServiceConfig{Endpoint: "https://hf.example"},
	}).Validate(); err != nil {
		t.Fatalf("expected valid endpoint, got: %v", err)
	}
	if err := (&TestDataRefServiceConfig{
		HF: &HFTestDataRefServiceConfig{Endpoint: "ftp://hf.example"},
	}).Validate(); err == nil {
		t.Fatal("expected invalid endpoint scheme to be rejected")
	}
	if err := (&TestDataRefServiceConfig{
		HF: &HFTestDataRefServiceConfig{Endpoint: "http://hf.example"},
	}).Validate(); err == nil {
		t.Fatal("expected http endpoint to be rejected")
	}
	if err := (&TestDataRefServiceConfig{
		HF: &HFTestDataRefServiceConfig{Endpoint: "not-a-url"},
	}).Validate(); err == nil {
		t.Fatal("expected malformed endpoint to be rejected")
	}
}

func TestServiceConfigEffectiveTestDataRefSettings(t *testing.T) {
	sc := &ServiceConfig{
		TestDataRef: &TestDataRefServiceConfig{
			DownloadTimeout: 12 * time.Minute,
			HF:              &HFTestDataRefServiceConfig{Endpoint: "https://hub.example"},
		},
	}
	if got := sc.EffectiveTestDataRefDownloadTimeout(); got != 12*time.Minute {
		t.Fatalf("download timeout = %v", got)
	}
	if got := sc.EffectiveHFHubEndpoint(); got != "https://hub.example" {
		t.Fatalf("hf endpoint = %q", got)
	}
}
