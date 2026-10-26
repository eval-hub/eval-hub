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
	case "lighteval":
		p["num_examples"], p["num_few_shot"], p["batch_size"] = 1, 0, 1
		f["notes"] = "One example per underlying task. Requires an endpoint supporting this task's request type and a compatible tokenizer."
	case "garak", "garak-kfp":
		p["num_examples"] = 1
		f["notes"] = "One example per probe; broad probe groups still invoke multiple probes. garak-kfp additionally requires Data Science Pipelines."
		if benchmark == "intents" {
			p["intents_models"] = map[string]any{"judge": map[string]any{"name": "${JUDGE_MODEL_NAME}", "url": "${JUDGE_BASE_URL}"}}
			p["sdg_model"], p["sdg_api_base"] = "${MODEL_NAME}", "${MODEL_URL}"
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
