package config_test

import (
	"io"
	"log/slog"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
	"github.com/eval-hub/eval-hub/internal/testhelpers"
)

func TestBundledClassificationMetadata(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	validate := testhelpers.NewValidator(t)
	const configDir = "../../../config"

	providers, err := config.LoadProviderConfigs(logger, validate, configDir)
	if err != nil {
		t.Fatalf("load bundled providers: %v", err)
	}
	if len(providers) == 0 {
		t.Fatal("no bundled providers loaded")
	}
	for providerID, provider := range providers {
		for _, benchmark := range provider.Benchmarks {
			if len(benchmark.Domains) == 0 || len(benchmark.Tasks) == 0 {
				t.Errorf("%s/%s: domains and tasks must be set", providerID, benchmark.ID)
			}
			// The selected Inspect task determines these fields at runtime.
			if providerID == "inspect" && benchmark.ID == "inspect/custom" {
				continue
			}
			if len(benchmark.Modalities) == 0 || len(benchmark.EvaluationTargets) == 0 {
				t.Errorf("%s/%s: modalities and evaluation_targets must be set", providerID, benchmark.ID)
			}
		}
	}

	collections, err := config.LoadCollectionConfigs(logger, validate, configDir)
	if err != nil {
		t.Fatalf("load bundled collections: %v", err)
	}
	if len(collections) == 0 {
		t.Fatal("no bundled collections loaded")
	}
	curated := false
	for collectionID, collection := range collections {
		if len(collection.Domains) == 0 || len(collection.Tasks) == 0 ||
			len(collection.Modalities) == 0 || len(collection.EvaluationTargets) == 0 {
			t.Errorf("%s: all four classification fields must be set", collectionID)
		}
		if collection.CurationOrder > 0 {
			curated = true
		}
	}
	if !curated {
		t.Error("at least one bundled collection must be curated")
	}
}
