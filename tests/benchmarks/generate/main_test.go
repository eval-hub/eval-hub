package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/eval-hub/eval-hub/tests/benchmarks/internal/fixturecheck"
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
			if fields := fixturecheck.ConnectionOptions(p); len(fields) != 0 {
				t.Errorf("model connections must come from the Secret: %v", fields)
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
			if fields := fixturecheck.ConnectionOptions(parameters); len(fields) != 0 {
				t.Errorf("model connections must come from the Secret: %v", fields)
			}
		})
	}
}

func TestRoleProfilesDoNotEmitConnectionParameters(t *testing.T) {
	paths, err := filepath.Glob("../../../config/providers/*.yaml")
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
			Benchmarks []struct {
				ID string `yaml:"id"`
			} `yaml:"benchmarks"`
		}
		if err := yaml.Unmarshal(data, &catalog); err != nil {
			t.Fatal(err)
		}
		if !slices.Contains(fixturecheck.Providers(), catalog.ID) {
			continue
		}
		seen[catalog.ID] = true
		if len(catalog.Benchmarks) == 0 {
			t.Fatalf("no benchmarks found for %s", catalog.ID)
		}
		for _, benchmark := range catalog.Benchmarks {
			t.Run(catalog.ID+"/"+benchmark.ID, func(t *testing.T) {
				f, err := minimalFixture(catalog.ID, benchmark.ID)
				if err != nil {
					t.Fatal(err)
				}
				if fields := fixturecheck.ConnectionOptions(f["parameters"]); len(fields) != 0 {
					t.Errorf("generated parameters contain model connections: %v", fields)
				}
			})
		}
	}
	for _, provider := range fixturecheck.Providers() {
		if !seen[provider] {
			t.Errorf("no catalog found for %s", provider)
		}
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
					t.Fatalf("grader must select a model without inline connection options: %v", roles["grader"])
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
