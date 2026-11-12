package handlers

import (
	"errors"
	"strings"
	"testing"
)

func TestParseCollectionGenerationOptions(t *testing.T) {
	t.Run("applies documented defaults and trims goal", func(t *testing.T) {
		options, err := parseCollectionGenerationOptions([]byte(`{"evaluation_goal":"  evaluate safety  "}`))
		if err != nil {
			t.Fatalf("parseCollectionGenerationOptions: %v", err)
		}
		if options.EvaluationGoal != "evaluate safety" || options.MaxBenchmarks != 12 || options.Strictness != "moderate" || options.GeneratorModelID != collectionGenerationMockModelID || options.GenerationEffort != "standard" {
			t.Fatalf("unexpected normalized options: %+v", options)
		}
	})

	t.Run("accepts every declared enum and integral JSON number representation", func(t *testing.T) {
		for _, effort := range []string{"low", "standard", "high"} {
			for _, strictness := range []string{"lenient", "moderate", "strict"} {
				for _, benchmarkCount := range []string{"1", "1.0", "1e0", "50"} {
					body := `{"evaluation_goal":"goal","provider_filter":"inspect,lighteval","max_benchmarks":` + benchmarkCount + `,"strictness":"` + strictness + `","generator_model_id":"mock-generator","generation_effort":"` + effort + `"}`
					options, err := parseCollectionGenerationOptions([]byte(body))
					if err != nil {
						t.Fatalf("valid request effort=%s strictness=%s max=%s: %v", effort, strictness, benchmarkCount, err)
					}
					if options.GenerationEffort != effort || options.Strictness != strictness || options.MaxBenchmarks < 1 || options.MaxBenchmarks > 50 || options.ProviderFilter != "inspect,lighteval" {
						t.Fatalf("request not normalized as specified: %+v", options)
					}
				}
			}
		}
	})

	tests := []struct {
		name string
		body string
	}{
		{name: "invalid JSON", body: `{"evaluation_goal":`},
		{name: "non-object", body: `[]`},
		{name: "null object", body: `null`},
		{name: "missing required goal", body: `{}`},
		{name: "null goal", body: `{"evaluation_goal":null}`},
		{name: "blank goal", body: `{"evaluation_goal":" \t\n "}`},
		{name: "goal too long before trimming", body: `{"evaluation_goal":" ` + strings.Repeat("x", 2000) + ` "}`},
		{name: "null provider filter", body: `{"evaluation_goal":"goal","provider_filter":null}`},
		{name: "empty provider filter", body: `{"evaluation_goal":"goal","provider_filter":""}`},
		{name: "max benchmark below minimum", body: `{"evaluation_goal":"goal","max_benchmarks":0}`},
		{name: "max benchmark above maximum", body: `{"evaluation_goal":"goal","max_benchmarks":51}`},
		{name: "fractional max benchmark", body: `{"evaluation_goal":"goal","max_benchmarks":1.5}`},
		{name: "null max benchmark", body: `{"evaluation_goal":"goal","max_benchmarks":null}`},
		{name: "string max benchmark", body: `{"evaluation_goal":"goal","max_benchmarks":"12"}`},
		{name: "invalid strictness", body: `{"evaluation_goal":"goal","strictness":"high"}`},
		{name: "invalid effort", body: `{"evaluation_goal":"goal","generation_effort":"fast"}`},
		{name: "unknown field", body: `{"evaluation_goal":"goal","model_url":"https://example.test"}`},
		{name: "model id too long before trimming", body: `{"evaluation_goal":"goal","generator_model_id":"` + strings.Repeat(" ", 1) + "mock-generator" + strings.Repeat(" ", 116) + `"}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := parseCollectionGenerationOptions([]byte(test.body)); err == nil {
				t.Fatalf("expected invalid request %s to fail", test.body)
			}
		})
	}

	t.Run("normalizes whitespace-only provider filter to omitted", func(t *testing.T) {
		options, err := parseCollectionGenerationOptions([]byte(`{"evaluation_goal":"goal","provider_filter":"   "}`))
		if err != nil {
			t.Fatalf("whitespace provider_filter is schema-valid: %v", err)
		}
		if options.ProviderFilter != "" {
			t.Fatalf("provider_filter = %q, want empty normalized filter", options.ProviderFilter)
		}
	})

	t.Run("rejects an unavailable model without exposing configuration", func(t *testing.T) {
		_, err := parseCollectionGenerationOptions([]byte(`{"evaluation_goal":"goal","generator_model_id":"private-profile"}`))
		if !errors.Is(err, errInvalidCollectionGenerationModel) {
			t.Fatalf("error = %v, want errInvalidCollectionGenerationModel", err)
		}
	})
}
