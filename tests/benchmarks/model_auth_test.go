package benchmarks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/eval-hub/eval-hub/pkg/api"
	"github.com/eval-hub/eval-hub/tests/benchmarks/internal/fixturecheck"
	"go.yaml.in/yaml/v4"
)

func TestRoleFixturesDoNotSerializeConnectionParameters(t *testing.T) {
	for _, provider := range fixturecheck.Providers() {
		paths, err := filepath.Glob(filepath.Join("testdata", provider, "*.json"))
		if err != nil {
			t.Fatal(err)
		}
		if len(paths) == 0 {
			t.Fatalf("no fixtures found for %s", provider)
		}
		for _, path := range paths {
			t.Run(path, func(t *testing.T) {
				lookup := func(key string) (string, bool) {
					value, ok := fixtureEnv(key)
					if !ok {
						t.Fatalf("unexpected fixture environment input %q", key)
					}
					return value, ok
				}
				f, err := loadFixture(path, lookup)
				if err != nil {
					t.Fatal(err)
				}
				job := f.jobConfig(provider, "fixture-benchmark", api.ModelRef{
					Name: "target", URL: "https://target.example/v1", Auth: &api.ModelAuth{SecretRef: "model-auth"},
				})
				if job.Model.Auth == nil || job.Model.Auth.SecretRef != "model-auth" {
					t.Fatal("model Secret reference was lost")
				}
				body, err := json.Marshal(job)
				if err != nil {
					t.Fatal(err)
				}
				var submitted api.EvaluationJobConfig
				if err := json.Unmarshal(body, &submitted); err != nil {
					t.Fatal(err)
				}
				for _, benchmark := range submitted.Benchmarks {
					if fields := fixturecheck.ConnectionOptions(benchmark.Parameters); len(fields) != 0 {
						t.Errorf("submitted parameters contain model connection fields: %v", fields)
					}
				}
			})
		}
	}
}

func TestRoleProviderCatalogsDoNotAdvertiseConnectionParameters(t *testing.T) {
	paths, err := filepath.Glob("../../config/providers/*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]bool)
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var catalog struct {
			ID         string `yaml:"id"`
			Parameters []struct {
				Name string `yaml:"name"`
			} `yaml:"parameters"`
		}
		if err := yaml.Unmarshal(data, &catalog); err != nil {
			t.Fatal(err)
		}
		if !slices.Contains(fixturecheck.Providers(), catalog.ID) {
			continue
		}
		seen[catalog.ID] = true
		t.Run(catalog.ID, func(t *testing.T) {
			for _, parameter := range catalog.Parameters {
				if fixturecheck.IsConnectionOption(parameter.Name) {
					t.Errorf("catalog advertises model connection parameter %q", parameter.Name)
				}
			}
		})
	}
	for _, provider := range fixturecheck.Providers() {
		if !seen[provider] {
			t.Errorf("no catalog found for %s", provider)
		}
	}
}
