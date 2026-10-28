// Command generate creates missing smoke fixtures from the provider catalog.
// Run from the repository root:
// go run ./tests/benchmarks/generate
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v4"
)

func main() {
	providersDir := flag.String("providers", "config/providers", "provider YAML directory")
	outputDir := flag.String("output", "tests/benchmarks/testdata", "fixture directory")
	flag.Parse()
	if err := generate(*providersDir, *outputDir); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func fileID(id string) string { return strings.ReplaceAll(url.PathEscape(id), ".", "%2E") }

func generate(providersDir, outputDir string) error {
	files, err := filepath.Glob(filepath.Join(providersDir, "*.yaml"))
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("no provider YAML files in %s", providersDir)
	}
	created, total := 0, 0
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var provider struct {
			ID         string `yaml:"id"`
			Benchmarks []struct {
				ID string `yaml:"id"`
			} `yaml:"benchmarks"`
		}
		if err := yaml.Unmarshal(data, &provider); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		if provider.ID == "" {
			return fmt.Errorf("%s: missing provider ID", path)
		}
		for _, benchmark := range provider.Benchmarks {
			if benchmark.ID == "" {
				return fmt.Errorf("%s: missing benchmark ID", path)
			}
			total++
			fixture, err := minimalFixture(provider.ID, benchmark.ID)
			if err != nil {
				return err
			}
			path := filepath.Join(outputDir, fileID(provider.ID), fileID(benchmark.ID)+".json")
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return err
			}
			data, err := json.MarshalIndent(fixture, "", "  ")
			if err != nil {
				return err
			}
			// Preserve reviewed, hand-tuned parameters when regenerating.
			file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
			if os.IsExist(err) {
				continue
			}
			if err != nil {
				return err
			}
			_, writeErr := file.Write(append(data, '\n'))
			closeErr := file.Close()
			if err := errors.Join(writeErr, closeErr); err != nil {
				// Incomplete fixtures must not be preserved as reviewed on the next run.
				return errors.Join(err, os.Remove(path))
			}
			created++
		}
	}
	fmt.Printf("%d providers, %d benchmarks; created %d fixtures (existing fixtures preserved)\n", len(files), total, created)
	return nil
}

func minimalFixture(provider, benchmark string) (map[string]any, error) {
	p := map[string]any{}
	f := map[string]any{"parameters": p, "timeout": "5m"}
	switch provider {
	case "lm_evaluation_harness":
		p["num_examples"], p["num_fewshot"] = 1, 0
		f["notes"] = "One example per underlying task; grouped benchmarks may expand into multiple tasks. Model endpoint must support the benchmark's request type (including loglikelihood where applicable)."
		if benchmark == "longbenchv2" {
			p["max_length"] = 4096
			p["loglikelihood_context_overflow"] = "truncate"
			f["max_length_env"] = "MODEL_MAX_LENGTH"
			f["notes"] = "One LongBench example with a 4096-token smoke context budget. Set MODEL_MAX_LENGTH to the served model's context limit (integer >= 2); the budget must not exceed that limit. This short default does not validate full long-context accuracy."
		}
	case "lighteval":
		p["num_examples"], p["num_few_shot"], p["batch_size"] = 1, 0, 1
		f["notes"] = "One example per underlying task. Requires an endpoint supporting this task's request type and a compatible tokenizer."
		if benchmark == "math" || benchmark == "math:algebra" || benchmark == "math:counting_and_probability" {
			p["generation_parameters"] = map[string]any{"temperature": 0.1}
		}
	case "garak", "garak-kfp":
		p["num_examples"] = 1
		f["notes"] = "One example per probe; broad probe groups still invoke multiple probes. garak-kfp additionally requires Data Science Pipelines."
		if provider == "garak-kfp" {
			p["kfp_config"] = map[string]any{"endpoint": "${KFP_ENDPOINT}", "namespace": "${KFP_NAMESPACE}", "s3_secret_name": "${KFP_S3_SECRET_NAME}"}
			f["notes"] = "Requires KFP_ENDPOINT, KFP_NAMESPACE, and KFP_S3_SECRET_NAME for a working Data Science Pipelines deployment and Data Connection Secret. Configure KFP authentication through the model auth Secret. One example per probe; broad groups still invoke multiple probes."
		}
		if benchmark == "intents" {
			p["intents_models"] = map[string]any{"judge": map[string]any{"name": "${JUDGE_MODEL_NAME}", "url": "${JUDGE_BASE_URL}"}}
			p["sdg_model"], p["sdg_api_base"] = "openai/${MODEL_NAME}", "${MODEL_URL}"
		}
	case "guidellm":
		p["num_examples"], p["max_requests"], p["max_seconds"] = 1, 1, 10
		p["data"], p["warmup"], p["cooldown"] = "prompt_tokens=16,output_tokens=8", 0, 0
		// Preserve benchmark identity: do not change all profiles to a serial run.
		profile := benchmark
		if profile == "quick_perf_test" || profile == "comprehensive_perf_test" {
			profile = "sweep"
		}
		p["profile"] = profile
		if profile == "concurrent" || profile == "constant" || profile == "poisson" {
			p["rate"] = 1
		}
	case "databench", "ifbench", "followbench":
		p["num_examples"], p["max_tokens"], p["temperature"], p["request_timeout"] = 1, 128, 0, 60
		if provider == "ifbench" {
			p["max_concurrent"] = 1
		}
		if provider == "followbench" {
			p["judge_model"] = "${JUDGE_MODEL_NAME}"
			f["notes"] = "Requires model.auth.secret_ref to contain judge_api-key and judge_url for the independent judge connection; judge_model cannot be combined with inline judge_url or judge_api_key."
		}
	case "ruler":
		p["num_samples"], p["context_lengths"], p["batch_size"], p["tokens_to_generate"] = 1, []int{4096}, 1, 32
	case "tooleval":
		p["num_tasks"], p["tool_server_url"] = 1, "${TOOL_SERVER_URL}"
		f["notes"] = "Requires a reachable ToolBench tool server and a model supporting tool calls; preserves the benchmark's default step count."
	case "inspect":
		p["num_examples"], p["epochs"], p["max_tasks"] = 1, 1, 1
		if strings.Contains(benchmark, "/petri-") || strings.Contains(benchmark, "/bloom-") {
			p["max_turns"] = 1
			p["auditor_model"], p["auditor_base_url"] = "${MODEL_NAME}", "${MODEL_URL}"
			p["judge_model"], p["judge_base_url"], p["judge_api_key"] = "${JUDGE_MODEL_NAME}", "${JUDGE_BASE_URL}", "${JUDGE_API_KEY}"
		}
		if benchmark == "inspect/hle" || benchmark == "inspect/cyberseceval-2-pi" {
			p["judge_model"] = "${JUDGE_MODEL_NAME}"
		}
		if benchmark == "inspect/strong-reject" {
			p["grader_model"], p["grader_base_url"] = "${JUDGE_MODEL_NAME}", "${JUDGE_BASE_URL}"
		}
		if benchmark == "inspect/custom" {
			p["task"] = "${INSPECT_CUSTOM_TASK}"
		}
		if benchmark == "inspect/bloom-custom" {
			p["bloom_template"] = "${INSPECT_BLOOM_TEMPLATE}"
		}
		f["notes"] = "One sample per task. Agent/code benchmarks may require sandbox and external services; configure those in this fixture for the target environment. Petri/Bloom require JUDGE_MODEL_NAME, JUDGE_BASE_URL, and JUDGE_API_KEY for the judge. HLE/CyberSecEval require judge_api-key and judge_url in model.auth.secret_ref for an independent judge connection. StrongREJECT requires Secret-backed OPENAI_JUDGE_API_KEY in the adapter environment."
		// These catalog IDs name task families rather than registered Inspect
		// tasks. Select one representative task without changing the catalog ID.
		tasks := map[string]string{
			"inspect/arc":                    "arc_easy",
			"inspect/mmlu":                   "mmlu_0_shot",
			"inspect/gpqa":                   "gpqa_diamond",
			"inspect/wmdp":                   "wmdp_bio",
			"inspect/sad":                    "sad_facts_llms",
			"inspect/gdm-stealth":            "gdm_oversight_pattern",
			"inspect/gdm-self-reasoning":     "gdm_self_reasoning_approved_directories",
			"inspect/gdm-self-proliferation": "gdm_sp01_e2e",
			"inspect/cyberseceval-2":         "cyse2_interpreter_abuse",
		}
		if task, ok := tasks[benchmark]; ok {
			p["task"] = "inspect_evals/" + task
			f["notes"] = fmt.Sprintf("One example from representative task inspect_evals/%s. Agent/code tasks still require their sandbox and external services; GPQA requires dataset access.", task)
		}
		switch benchmark {
		case "inspect/mask":
			p["task_args"] = map[string]any{"binary_judge_model": "openai/${MODEL_NAME}", "numeric_judge_model": "openai/${MODEL_NAME}"}
			f["notes"] = "One smoke sample, using the target model and its endpoint/credentials for both judges instead of unavailable task-default models. Not an independent accuracy assessment."
		case "inspect/instrumental-eval", "inspect/agentic-misalignment":
			p["task_args"] = map[string]any{"grader_model": "openai/${MODEL_NAME}"}
			f["notes"] = "One smoke sample, using the target model and its endpoint/credentials as grader instead of an unavailable task-default model. Not an independent accuracy assessment."
		case "inspect/cyberseceval-2":
			// Disable the task's default judge so Inspect resolves the grader role.
			p["task_args"] = map[string]any{"judge_llm": "null"}
			p["model_roles"] = map[string]any{"grader": `{"model":"openai/${JUDGE_MODEL_NAME}","base_url":"${JUDGE_BASE_URL}","api_key":"${JUDGE_API_KEY}","model_args":{"responses_api":false}}`}
			f["notes"] = "One interpreter-abuse sample. Requires JUDGE_MODEL_NAME, JUDGE_BASE_URL, and JUDGE_API_KEY for an independent OpenAI-compatible grader connection."
		case "inspect/make-me-pay":
			p["model_roles"] = map[string]any{"mark": "openai/${MODEL_NAME}"}
			p["task_args"] = map[string]any{"num_experiments": 1, "turn_cap": 2}
			f["notes"] = "One experiment with turn_cap=2 so the mark can respond, using the target model and its endpoint/credentials for the mark role instead of the default Google model. Smoke execution only."
		case "inspect/makemesay":
			p["task_args"] = map[string]any{"max_turns": 1}
			f["notes"] = "One sample with one game turn to bound smoke workload; not a full multi-turn accuracy assessment."
		}
	case "ragas":
		p["judge_model"] = "${JUDGE_MODEL_NAME}"
		p["embedding_model"] = "${RAGAS_EMBEDDING_MODEL}"
		p["num_examples"], p["max_workers"] = 1, 1
		f["test_data_ref_env"] = "RAGAS_TEST_DATA_REF"
		f["notes"] = "Stage a single complete RAG record as dataset.jsonl. Set RAGAS_EMBEDDING_MODEL. Configure embedding_api-key/embedding_url and optional judge_api-key/judge_url in MODEL_AUTH_SECRET_REF for sidecar routing."
	case "deepeval":
		p["eval_model_name"], p["eval_model_url"] = "${JUDGE_MODEL_NAME}", "${JUDGE_BASE_URL}"
		p["dataset_format"], p["max_concurrent"], p["retry_max_attempts"] = "jsonl", 1, 1
		f["test_data_ref_env"] = "DEEPEVAL_TEST_DATA_REF"
		f["notes"] = "Stage exactly one record in a JSONL file with input, actual_output, expected_output, context, and retrieval_context fields. No adapter sample cap; input size bounds work. Requires a structured-output judge."
	case "ibm-clear":
		p["provider"], p["eval_model_name"], p["inference_backend"], p["max_workers"] = "openai", "${MODEL_NAME}", "endpoint", 1
		f["test_data_ref_env"] = "CLEAR_TEST_DATA_REF"
		f["notes"] = "Stage a single short agent trace in CLEAR's expected trace format; the adapter has no sample limit."
	case "atif":
		f["model"] = map[string]any{"name": "${JUDGE_MODEL_NAME}", "url": "${JUDGE_BASE_URL}"}
		p["max_trajectory_files"], p["max_total_steps"], p["max_steps_per_trajectory"], p["concurrency_limit"] = 1, 1, 1, 1
		p["scoring_mode"], p["benchmark_name"], p["judge_max_attempts"], p["job_timeout_seconds"] = "benchmark", "humanevalfix", 1, 120
		f["test_data_ref_env"], f["experiment"] = "ATIF_TEST_DATA_REF", true
		f["notes"] = "Stage one valid one-step ATIF trajectory; requires configured MLflow for artifact persistence."
	case "mteb":
		p["batch_size"], p["languages"], p["co2_tracker"] = 1, []string{"eng"}, false
		// Smallest representative task from the corresponding adapter preset.
		tasks := map[string]string{"mteb_sts": "STS12", "mteb_retrieval": "NFCorpus", "mteb_classification": "EmotionClassification", "mteb_clustering": "BiorxivClusteringP2P", "mteb_reranking": "SciDocsRR"}
		if task, ok := tasks[benchmark]; ok {
			p["tasks"] = []string{task}
		}
		f["model"] = map[string]any{"name": "${MTEB_MODEL_NAME}", "url": "${MODEL_URL}"}
		f["unbounded"] = true
		f["notes"] = "MTEB loads an embedding model locally rather than using MODEL_URL. No supported per-task sample cap; BENCHMARK_ALLOW_UNBOUNDED=1 explicitly allows a full selected task within the deadline."
	case "nemo-guardrails":
		p["workers"] = 1
		f["unbounded"] = true
		f["notes"] = "Adapter ignores num_examples and uses packaged dataset eval_limit values; requires BENCHMARK_ALLOW_UNBOUNDED=1. Install the benchmark's NeMo config and required guardrail models. A short smoke run cannot be guaranteed without upstream sample-limit support."
	default:
		return nil, fmt.Errorf("provider %q needs a reviewed minimal workload profile", provider)
	}
	return f, nil
}
