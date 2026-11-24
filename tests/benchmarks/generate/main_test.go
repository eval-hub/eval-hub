package main

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"go.yaml.in/yaml/v4"
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

func TestHLESmokeScorerMatchesCollection(t *testing.T) {
	data, err := os.ReadFile("../../../config/collections/knowledge-reasoning-v1.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var collection struct {
		Benchmarks []struct {
			ID         string         `yaml:"id"`
			Parameters map[string]any `yaml:"parameters"`
		} `yaml:"benchmarks"`
	}
	if err := yaml.Unmarshal(data, &collection); err != nil {
		t.Fatal(err)
	}
	var expected any
	for _, benchmark := range collection.Benchmarks {
		if benchmark.ID == "inspect/hle" {
			expected = benchmark.Parameters["task_args"].(map[string]any)["scorer"]
		}
	}
	if expected == nil {
		t.Fatal("collection must define the supported HLE scorer configuration")
	}
	want, err := json.Marshal(expected)
	if err != nil {
		t.Fatal(err)
	}
	generated, err := minimalFixture("inspect", "inspect/hle")
	if err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile("../testdata/inspect/inspect%2Fhle.json")
	if err != nil {
		t.Fatal(err)
	}
	var checkedIn map[string]any
	if err := json.Unmarshal(data, &checkedIn); err != nil {
		t.Fatal(err)
	}
	for name, fixture := range map[string]map[string]any{"generated": generated, "checked-in": checkedIn} {
		t.Run(name, func(t *testing.T) {
			parameters := fixture["parameters"].(map[string]any)
			args, ok := parameters["task_args"].(map[string]any)
			if !ok {
				t.Fatal("HLE smoke must explicitly select the configured grader instead of task defaults")
			}
			got, err := json.Marshal(args["scorer"])
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != string(want) {
				t.Fatalf("HLE smoke scorer must match the single-grader collection configuration:\ngot  %s\nwant %s", got, want)
			}
		})
	}
}

func TestJudgeProfiles(t *testing.T) {
	for _, tc := range []struct {
		provider, benchmark string
		fields              map[string]string
	}{
		{"inspect", "inspect/petri-sycophancy", map[string]string{"judge_model": "${JUDGE_MODEL_NAME}"}},
		{"inspect", "inspect/bloom-custom", map[string]string{"judge_model": "${JUDGE_MODEL_NAME}"}},
		{"inspect", "inspect/hle", map[string]string{"judge_model": "${JUDGE_MODEL_NAME}"}},
		{"inspect", "inspect/cyberseceval-2-pi", map[string]string{"judge_model": "${JUDGE_MODEL_NAME}"}},
		{"inspect", "inspect/strong-reject", map[string]string{"grader_model": "${JUDGE_MODEL_NAME}"}},
		{"followbench", "followbench", map[string]string{"judge_model": "${JUDGE_MODEL_NAME}"}},
		{"ragas", "ragas_rag_default", map[string]string{"judge_model": "${JUDGE_MODEL_NAME}"}},
		{"deepeval", "deepeval-faithfulness", map[string]string{"eval_model_name": "${JUDGE_MODEL_NAME}"}},
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
		})
	}
	for _, provider := range []string{"garak", "garak-kfp"} {
		f, err := minimalFixture(provider, "intents")
		if err != nil {
			t.Fatal(err)
		}
		p := f["parameters"].(map[string]any)
		judge := p["intents_models"].(map[string]any)["judge"].(map[string]any)
		if judge["name"] != "${JUDGE_MODEL_NAME}" || p["sdg_model"] != "openai/${MODEL_NAME}" {
			t.Fatalf("%s must separate judge inputs from generation inputs: %v", provider, p)
		}
		if provider == "garak" {
			if _, ok := judge["url"]; ok {
				t.Fatal("garak judge URL must come from the model-auth Secret")
			}
			if p["sdg_max_concurrency"] != 1 {
				t.Fatal("garak SDG requests must run serially")
			}
			if _, ok := p["sdg_api_base"]; ok {
				t.Fatal("garak SDG URL must come from the model-auth Secret")
			}
		} else if judge["url"] != "${JUDGE_BASE_URL}" || p["sdg_api_base"] != "${MODEL_URL}" {
			t.Fatalf("garak-kfp requires its existing endpoint bindings: %v", p)
		}
	}
}

func TestGarakTimeout(t *testing.T) {
	for _, tc := range []struct {
		provider string
		want     string
	}{
		{provider: "garak", want: "15m"},
		{provider: "garak-kfp", want: "5m"},
	} {
		t.Run(tc.provider, func(t *testing.T) {
			f, err := minimalFixture(tc.provider, "quality")
			if err != nil {
				t.Fatal(err)
			}
			if got := f["timeout"]; got != tc.want {
				t.Fatalf("timeout = %v; want %s", got, tc.want)
			}
		})
	}
}

func TestRulerUsesTokenizerPathEnv(t *testing.T) {
	f, err := minimalFixture("ruler", "niah-single-noise")
	if err != nil {
		t.Fatal(err)
	}
	if got := f["tokenizer_path_env"]; got != "RULER_TOKENIZER" {
		t.Fatalf("tokenizer_path_env = %v; want RULER_TOKENIZER", got)
	}
	parameters := f["parameters"].(map[string]any)
	if _, pinned := parameters["tokenizer_path"]; pinned {
		t.Fatalf("ruler fixture should defer tokenizer_path to environment or adapter fallback: %v", parameters)
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

func TestRagasProfilesSelectEmbeddingModel(t *testing.T) {
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
				if roles["grader"] != "openai/${JUDGE_MODEL_NAME}" {
					t.Fatalf("grader must select the configured judge model: %v", roles["grader"])
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
