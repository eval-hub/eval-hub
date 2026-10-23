package main

import "testing"

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
		{"ragas", "ragas_rag_default", map[string]string{"judge_model": "${JUDGE_MODEL_NAME}", "judge_url": "${JUDGE_BASE_URL}", "judge_api_key": "${JUDGE_API_KEY}"}},
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
		if judge["name"] != "${JUDGE_MODEL_NAME}" || judge["url"] != "${JUDGE_BASE_URL}" || p["sdg_model"] != "${MODEL_NAME}" {
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
