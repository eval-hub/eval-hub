package config_test

import (
	"io"
	"log/slog"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
	"github.com/eval-hub/eval-hub/internal/testhelpers"
)

func TestBundledJudgeCollectionsDeclareFlatInputs(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	collections, err := config.LoadCollectionConfigs(logger, testhelpers.NewValidator(t), "../../../config")
	if err != nil {
		t.Fatalf("load bundled collections: %v", err)
	}

	tests := []struct {
		collectionID string
		providerID   string
		benchmarkID  string
	}{
		{"knowledge-reasoning-v1", "inspect", "inspect/hle"},
		{"instruction-output-v1", "followbench", "followbench"},
		{"trustworthiness-v1", "inspect", "inspect/cyberseceval-2-pi"},
	}
	for _, tt := range tests {
		t.Run(tt.collectionID, func(t *testing.T) {
			collection, ok := collections[tt.collectionID]
			if !ok {
				t.Fatalf("collection %s not loaded", tt.collectionID)
			}
			for _, benchmark := range collection.Benchmarks {
				if benchmark.ProviderID != tt.providerID || benchmark.ID != tt.benchmarkID {
					continue
				}
				if value, ok := benchmark.Parameters["judge_model"]; !ok || value != "" {
					t.Errorf("judge_model = %v (present %t), want an empty string form field", value, ok)
				}
				for _, key := range []string{"judge_route", "judge_url", "judge_api_key"} {
					if _, ok := benchmark.Parameters[key]; ok {
						t.Errorf("%s must not be a collection parameter", key)
					}
				}
				return
			}
			t.Errorf("benchmark %s/%s not found", tt.providerID, tt.benchmarkID)
		})
	}
}
