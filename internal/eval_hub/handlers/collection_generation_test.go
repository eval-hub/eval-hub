package handlers

import (
	"strings"
	"testing"
)

const generationTestModel = `"model":{"name":"generator","url":"https://generator.example/v1"}`

func TestParseCollectionGenerationRequest(t *testing.T) {
	t.Run("applies documented defaults and trims goal", func(t *testing.T) {
		request, err := parseCollectionGenerationRequest([]byte(`{"evaluation_goal":"  evaluate safety  ",` + generationTestModel + `}`))
		if err != nil {
			t.Fatalf("parseCollectionGenerationRequest: %v", err)
		}
		if request.EvaluationGoal != "evaluate safety" || request.Model.Name != "generator" || request.Model.URL != "https://generator.example/v1" || request.MaxBenchmarks != 12 || request.Strictness != "moderate" || request.GenerationEffort != "standard" {
			t.Fatalf("unexpected normalized request: %+v", request)
		}
	})

	t.Run("accepts every declared enum and integral JSON number representation", func(t *testing.T) {
		for _, effort := range []string{"low", "standard", "high"} {
			for _, strictness := range []string{"lenient", "moderate", "strict"} {
				for _, benchmarkCount := range []string{"1", "1.0", "1e0", "100"} {
					body := `{"evaluation_goal":"goal",` + generationTestModel + `,"provider_filter":"inspect,lighteval","max_benchmarks":` + benchmarkCount + `,"strictness":"` + strictness + `","generation_effort":"` + effort + `"}`
					request, err := parseCollectionGenerationRequest([]byte(body))
					if err != nil {
						t.Fatalf("valid request effort=%s strictness=%s max=%s: %v", effort, strictness, benchmarkCount, err)
					}
					if request.GenerationEffort != effort || request.Strictness != strictness || request.MaxBenchmarks < 1 || request.MaxBenchmarks > 100 || request.ProviderFilter != "inspect,lighteval" {
						t.Fatalf("request not normalized as specified: %+v", request)
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
		{name: "null goal", body: `{"evaluation_goal":null,` + generationTestModel + `}`},
		{name: "blank goal", body: `{"evaluation_goal":" \t\n ",` + generationTestModel + `}`},
		{name: "goal too long before trimming", body: `{"evaluation_goal":" ` + strings.Repeat("x", 10_000) + ` ",` + generationTestModel + `}`},
		{name: "missing model", body: `{"evaluation_goal":"goal"}`},
		{name: "null model", body: `{"evaluation_goal":"goal","model":null}`},
		{name: "model without name", body: `{"evaluation_goal":"goal","model":{"url":"https://generator.example/v1"}}`},
		{name: "model without url", body: `{"evaluation_goal":"goal","model":{"name":"generator"}}`},
		{name: "model with embedded credentials", body: `{"evaluation_goal":"goal","model":{"name":"generator","url":"https://key:password@generator.example/v1"}}`},
		{name: "model with unsupported scheme", body: `{"evaluation_goal":"goal","model":{"name":"generator","url":"file:///tmp/model"}}`},
		{name: "model with null auth", body: `{"evaluation_goal":"goal","model":{"name":"generator","url":"https://generator.example/v1","auth":null}}`},
		{name: "model with missing secret ref", body: `{"evaluation_goal":"goal","model":{"name":"generator","url":"https://generator.example/v1","auth":{}}}`},
		{name: "model with alternate auth field casing", body: `{"evaluation_goal":"goal","model":{"name":"generator","url":"https://generator.example/v1","Auth":{}}}`},
		{name: "model with alternate null auth field casing", body: `{"evaluation_goal":"goal","model":{"name":"generator","url":"https://generator.example/v1","Auth":null}}`},
		{name: "model with alternate name field casing", body: `{"evaluation_goal":"goal","model":{"Name":"generator","url":"https://generator.example/v1"}}`},
		{name: "model auth with alternate secret ref field casing", body: `{"evaluation_goal":"goal","model":{"name":"generator","url":"https://generator.example/v1","auth":{"Secret_Ref":"generator-credentials"}}}`},
		{name: "unsupported plaintext credential", body: `{"evaluation_goal":"goal","model":{"name":"generator","url":"https://generator.example/v1","auth":{"api_key":"plaintext"}}}`},
		{name: "null provider filter", body: `{"evaluation_goal":"goal",` + generationTestModel + `,"provider_filter":null}`},
		{name: "empty provider filter", body: `{"evaluation_goal":"goal",` + generationTestModel + `,"provider_filter":""}`},
		{name: "max benchmark below minimum", body: `{"evaluation_goal":"goal",` + generationTestModel + `,"max_benchmarks":0}`},
		{name: "max benchmark above maximum", body: `{"evaluation_goal":"goal",` + generationTestModel + `,"max_benchmarks":101}`},
		{name: "fractional max benchmark", body: `{"evaluation_goal":"goal",` + generationTestModel + `,"max_benchmarks":1.5}`},
		{name: "null max benchmark", body: `{"evaluation_goal":"goal",` + generationTestModel + `,"max_benchmarks":null}`},
		{name: "string max benchmark", body: `{"evaluation_goal":"goal",` + generationTestModel + `,"max_benchmarks":"12"}`},
		{name: "invalid strictness", body: `{"evaluation_goal":"goal",` + generationTestModel + `,"strictness":"high"}`},
		{name: "invalid effort", body: `{"evaluation_goal":"goal",` + generationTestModel + `,"generation_effort":"fast"}`},
		{name: "unknown field", body: `{"evaluation_goal":"goal",` + generationTestModel + `,"model_url":"https://example.test"}`},
		{name: "old profile field", body: `{"evaluation_goal":"goal",` + generationTestModel + `,"generator_model_id":"old-profile"}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := parseCollectionGenerationRequest([]byte(test.body)); err == nil {
				t.Fatalf("expected invalid request %s to fail", test.body)
			}
		})
	}

	t.Run("accepts the documented goal and benchmark limits", func(t *testing.T) {
		goal := strings.Repeat("x", 10_000)
		body := `{"evaluation_goal":"` + goal + `",` + generationTestModel + `,"max_benchmarks":100}`
		request, err := parseCollectionGenerationRequest([]byte(body))
		if err != nil {
			t.Fatalf("maximum valid request was rejected: %v", err)
		}
		if len(request.EvaluationGoal) != 10_000 || request.MaxBenchmarks != 100 {
			t.Fatalf("maximum values were not preserved: goal length=%d max_benchmarks=%d", len(request.EvaluationGoal), request.MaxBenchmarks)
		}
	})

	t.Run("normalizes whitespace-only provider filter to omitted", func(t *testing.T) {
		request, err := parseCollectionGenerationRequest([]byte(`{"evaluation_goal":"goal",` + generationTestModel + `,"provider_filter":"   "}`))
		if err != nil {
			t.Fatalf("whitespace provider_filter is schema-valid: %v", err)
		}
		if request.ProviderFilter != "" {
			t.Fatalf("provider_filter = %q, want empty normalized filter", request.ProviderFilter)
		}
	})

	t.Run("preserves the caller's model connection and Secret reference", func(t *testing.T) {
		request, err := parseCollectionGenerationRequest([]byte(`{"evaluation_goal":"goal","model":{"name":"other-generator","url":"http://other.example/v1","auth":{"secret_ref":"generator-credentials"},"parameters":{"temperature":0}}}`))
		if err != nil {
			t.Fatalf("parseCollectionGenerationRequest: %v", err)
		}
		if request.Model.Name != "other-generator" || request.Model.URL != "http://other.example/v1" || request.Model.Auth == nil || request.Model.Auth.SecretRef != "generator-credentials" || request.Model.Parameters["temperature"] != float64(0) {
			t.Fatalf("model connection changed: %+v", request.Model)
		}
	})
}
