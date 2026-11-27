local test = import 'test.libsonnet';
local offlineParameters = if test.disconnected() then {
  hf_datasets_cache: '/test_data/hf-cache',
  hf_offline: true,
} else {
  hf_offline: false,
};

test.mergeOptional(
  {
    model: test.model(),
    name: 'test-evaluation-job-ethics-justice',
    benchmarks: [
      test.benchmark('ethics_justice', 'lm_evaluation_harness', {
        num_examples: 1,
        num_fewshot: 0,
        trust_remote_code: true,
        api_style: 'completions',
        tokenizer_backend: 'huggingface',
      } + offlineParameters),
    ],
    tags: ['benchmark-providers', 'lm_evaluation_harness'],
  },
  test.experiment('my-test-experiment-ethics-justice'),
)
