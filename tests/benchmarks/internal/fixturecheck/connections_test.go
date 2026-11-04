package fixturecheck

import (
	"reflect"
	"testing"
)

func TestConnectionOptions(t *testing.T) {
	// Include every form removed from the catalogs, and an arbitrary new role.
	for _, key := range []string{
		"api_key", "api-key", "apiKey", "api_base", "base_url", "baseUrl", "url",
		"target_api_key", "target_base_url", "target_url",
		"auditor_api_key", "auditor_base_url", "auditor_anthropic_base_url",
		"judge_api_key", "judge_base_url", "judge_url", "judge_anthropic_base_url",
		"scenarios_api_key", "scenarios_base_url", "realism_api_key", "realism_base_url",
		"anthropic_api_key", "embedding_api_key", "embedding_url", "eval_model_url",
		"grader_base_url", "generation_provider_url", "custom_role_api-key", "custom_role_url",
	} {
		t.Run(key, func(t *testing.T) {
			if got := ConnectionOptions(map[string]any{key: "legacy-value"}); !reflect.DeepEqual(got, []string{key}) {
				t.Fatalf("connection key escaped the guard: %v", got)
			}
		})
	}
	for _, tc := range []struct {
		name  string
		value any
		want  []string
	}{
		{"nested model args", map[string]any{"model_args": map[string]any{"base_url": "legacy-value"}}, []string{"model_args.base_url"}},
		{"nested list", map[string]any{"providers": []any{map[string]any{"apiKey": "legacy-value"}}}, []string{"providers[].apiKey"}},
		{"encoded role", map[string]any{"model_roles": map[string]any{"grader": `{"model":"openai/judge","model_args":{"api_key":"legacy-value"}}`}}, []string{"model_roles.grader.model_args.api_key"}},
		{"model choices and tool service", map[string]any{"judge_model": "judge", "embedding_model": "embedding", "tool_server_url": "https://tools.example", "temperature": 0.1}, nil},
		{"named role model", map[string]any{"model_roles": map[string]any{"grader": "openai/judge"}}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ConnectionOptions(tc.value); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("connection fields = %v; want %v", got, tc.want)
			}
		})
	}
}
