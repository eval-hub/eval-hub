package config_test

import (
	"io"
	"log/slog"
	"slices"
	"testing"

	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
	"github.com/eval-hub/eval-hub/internal/testhelpers"
)

// Primary metrics must match the adapter's callback names, which can differ from
// raw lm-eval metric keys that include a filter suffix.
func TestLMEvaluationHarnessPrimaryMetrics(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	providers, err := config.LoadProviderConfigs(logger, testhelpers.NewValidator(t), "../../../config")
	if err != nil {
		t.Fatalf("load provider configs: %v", err)
	}
	provider, ok := providers["lm_evaluation_harness"]
	if !ok {
		t.Fatal("lm_evaluation_harness provider not found")
	}

	expected := map[string]string{}
	expected["AraDiCE_openbookqa_eng"] = "acc_norm"
	expected["leaderboard_bbh_salient_translation_error_detection"] = "acc_norm"
	expected["AraDiCE_piqa_lev"] = "acc_norm"
	expected["arabic_leaderboard_arabic_mt_piqa"] = "acc_norm"
	expected["arabic_leaderboard_arabic_mt_piqa_light"] = "acc_norm"
	expected["arabic_mt_piqa"] = "acc_norm"
	expected["bigbench_gre_reading_comprehension_multiple_choice"] = "acc"
	expected["qasper_freeform"] = "f1_abstractive"
	expected["agieval_logiqa_zh"] = "acc_norm"
	expected["bbh"] = "exact_match"
	expected["bbh_cot_fewshot"] = "exact_match"
	expected["bbh_cot_fewshot_causal_judgement"] = "exact_match"
	expected["bbh_cot_fewshot_dyck_languages"] = "exact_match"
	expected["bbh_cot_fewshot_hyperbaton"] = "exact_match"
	expected["bbh_cot_fewshot_logical_deduction_three_objects"] = "exact_match"
	expected["bbh_cot_fewshot_navigate"] = "exact_match"
	expected["bbh_cot_fewshot_reasoning_about_colored_objects"] = "exact_match"
	expected["bbh_cot_fewshot_snarks"] = "exact_match"
	expected["bbh_cot_fewshot_tracking_shuffled_objects_five_objects"] = "exact_match"
	expected["bbh_cot_fewshot_web_of_lies"] = "exact_match"
	expected["bbh_cot_zeroshot"] = "exact_match"
	expected["bbh_cot_zeroshot_causal_judgement"] = "exact_match"
	expected["bbh_cot_zeroshot_dyck_languages"] = "exact_match"
	for _, benchmark := range provider.Benchmarks {
		metric, affected := expected[benchmark.ID]
		if !affected {
			continue
		}
		if benchmark.PrimaryScore == nil || benchmark.PrimaryScore.Metric != metric {
			t.Errorf("%s: primary metric = %v, want %q", benchmark.ID, benchmark.PrimaryScore, metric)
		}
		if !slices.Contains(benchmark.Metrics, metric) {
			t.Errorf("%s: primary metric %q absent from declared metrics %v", benchmark.ID, metric, benchmark.Metrics)
		}
		if benchmark.PassCriteria == nil || benchmark.PassCriteria.Threshold == nil {
			t.Errorf("%s: pass threshold is missing", benchmark.ID)
		}
		delete(expected, benchmark.ID)
	}
	for id := range expected {
		t.Errorf("affected benchmark %s not found", id)
	}
}
