package main

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

func TestFileID(t *testing.T) {
	for _, tc := range []struct{ id, want string }{
		{"arc:challenge", "arc%3Achallenge"},
		{"arc%3Achallenge", "arc%253Achallenge"},
		{"inspect/petri-sycophancy", "inspect%2Fpetri-sycophancy"},
		{"..", "%2E%2E"},
	} {
		t.Run(tc.id, func(t *testing.T) {
			if got := fileID(tc.id); got != tc.want {
				t.Fatalf("fileID(%q) = %q; want %q", tc.id, got, tc.want)
			}
		})
	}
}

func TestJudgeProfiles(t *testing.T) {
	for _, tc := range []struct {
		provider, benchmark string
		fields              map[string]string
	}{
		{"inspect", "inspect/petri-sycophancy", map[string]string{"judge_model": "${JUDGE_MODEL_NAME}", "judge_base_url": "${JUDGE_BASE_URL}", "judge_api_key": "${JUDGE_API_KEY}"}},
		{"inspect", "inspect/bloom-custom", map[string]string{"judge_model": "${JUDGE_MODEL_NAME}", "judge_base_url": "${JUDGE_BASE_URL}"}},
		{"inspect", "inspect/hle", map[string]string{"judge_model": "${JUDGE_MODEL_NAME}"}},
		{"inspect", "inspect/cyberseceval-2-pi", map[string]string{"judge_model": "${JUDGE_MODEL_NAME}"}},
		{"inspect", "inspect/strong-reject", map[string]string{"grader_model": "${JUDGE_MODEL_NAME}", "grader_base_url": "${JUDGE_BASE_URL}"}},
		{"followbench", "followbench", map[string]string{"judge_model": "${JUDGE_MODEL_NAME}"}},
		{"ragas", "ragas_rag_default", map[string]string{"judge_model": "${JUDGE_MODEL_NAME}"}},
		{"deepeval", "deepeval-faithfulness", map[string]string{"eval_model_name": "${JUDGE_MODEL_NAME}", "eval_model_url": "${JUDGE_BASE_URL}"}},
	} {
		t.Run(tc.provider+"/"+tc.benchmark, func(t *testing.T) {
			f, err := minimalFixture(tc.provider, tc.benchmark)
			if err != nil {
				t.Fatal(err)
			}
			p := f["parameters"].(map[string]any)
			for field, want := range tc.fields {
				if p[field] != want {
					t.Errorf("%s = %v; want %s", field, p[field], want)
				}
			}
			if tc.provider == "followbench" || tc.benchmark == "inspect/hle" || tc.benchmark == "inspect/cyberseceval-2-pi" {
				for _, field := range []string{"judge_url", "judge_base_url", "judge_api_key"} {
					if _, ok := p[field]; ok {
						t.Errorf("%s conflicts with Secret-backed judge routing", field)
					}
				}
			}
		})
	}
	for _, provider := range []string{"garak", "garak-kfp"} {
		f, err := minimalFixture(provider, "intents")
		if err != nil {
			t.Fatal(err)
		}
		p := f["parameters"].(map[string]any)
		judge := p["intents_models"].(map[string]any)["judge"].(map[string]any)
		if judge["name"] != "${JUDGE_MODEL_NAME}" || judge["url"] != "${JUDGE_BASE_URL}" || p["sdg_model"] != "openai/${MODEL_NAME}" {
			t.Fatalf("%s must separate judge inputs from generation inputs: %v", provider, p)
		}
	}
}

func TestATIFUsesJudgeModel(t *testing.T) {
	f, err := minimalFixture("atif", "atif-v1.0")
	if err != nil {
		t.Fatal(err)
	}
	model := f["model"].(map[string]any)
	if model["name"] != "${JUDGE_MODEL_NAME}" || model["url"] != "${JUDGE_BASE_URL}" {
		t.Fatalf("ATIF job model must select the trajectory judge: %v", model)
	}
}

func TestRagasProfilesRequireEmbeddingEndpoint(t *testing.T) {
	for _, benchmark := range []string{"ragas_rag_default", "ragas_rag_full"} {
		t.Run(benchmark, func(t *testing.T) {
			f, err := minimalFixture("ragas", benchmark)
			if err != nil {
				t.Fatal(err)
			}
			parameters := f["parameters"].(map[string]any)
			if parameters["embedding_model"] != "${RAGAS_EMBEDDING_MODEL}" {
				t.Fatalf("Ragas must select an embedding model: %v", parameters)
			}
			for _, key := range []string{"embedding_url", "judge_url", "judge_api_key"} {
				if _, exists := parameters[key]; exists {
					t.Errorf("%s must come from the model-auth Secret", key)
				}
			}
		})
	}
}

// TestInspectSmokeRoles verifies both participants run and scoring uses a separate connection.
func TestInspectSmokeRoles(t *testing.T) {
	for _, benchmark := range []string{"inspect/make-me-pay", "inspect/cyberseceval-2"} {
		t.Run(benchmark, func(t *testing.T) {
			f, err := minimalFixture("inspect", benchmark)
			if err != nil {
				t.Fatal(err)
			}
			p := f["parameters"].(map[string]any)
			args := p["task_args"].(map[string]any)
			roles := p["model_roles"].(map[string]any)
			if benchmark == "inspect/make-me-pay" {
				if args["turn_cap"].(int) < 2 || roles["mark"] != "openai/${MODEL_NAME}" {
					t.Fatalf("mark must get a turn using the configured model: %v", p)
				}
			} else {
				if args["judge_llm"] != "null" {
					t.Fatalf("task default must not bypass the grader role: %v", args)
				}
				var role map[string]any
				if err := json.Unmarshal([]byte(roles["grader"].(string)), &role); err != nil {
					t.Fatal(err)
				}
				if role["model"] != "openai/${JUDGE_MODEL_NAME}" {
					t.Errorf("unexpected grader model: %v", role["model"])
				}
				modelArgs := role["model_args"].(map[string]any)
				for key, want := range map[string]string{"base_url": "${JUDGE_BASE_URL}", "api_key": "${JUDGE_API_KEY}"} {
					if _, exists := role[key]; exists {
						t.Errorf("%s must be nested in model_args", key)
					}
					if modelArgs[key] != want {
						t.Errorf("model_args.%s = %v; want %s", key, modelArgs[key], want)
					}
				}
				if modelArgs["responses_api"] != false {
					t.Fatal("OpenAI-compatible judge must use chat completions")
				}
			}
			// Keep the checked-in fixtures consistent with regeneration.
			data, err := os.ReadFile("../testdata/inspect/" + fileID(benchmark) + ".json")
			if err != nil {
				t.Fatal(err)
			}
			var actual, expected map[string]any
			if err := json.Unmarshal(data, &actual); err != nil {
				t.Fatal(err)
			}
			generated, err := json.Marshal(f)
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(generated, &expected); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(actual, expected) {
				t.Fatal("checked-in fixture differs from generator")
			}
		})
	}
}
