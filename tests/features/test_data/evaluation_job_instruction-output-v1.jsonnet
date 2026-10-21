local test = import 'test.libsonnet';

test.oobCollectionRefJobWithBenchmarks(
  'test-evaluation-job-oob-collection',
  'instruction-output-v1',
  [
    test.benchmark('ifbench', 'ifbench', { num_examples: 1 }),
    // jsonschema_bench_easy uses chat templates, so its tokenizer must match the
    // chat-capable model instead of the helper's flan-t5 default.
    test.benchmark('jsonschema_bench_easy', 'lm_evaluation_harness', {
      num_examples: 1,
      tokenizer: test.model().name,
    }),
    test.benchmark('followbench', 'followbench', { num_examples: 1, judge_model: test.model().name }),
  ],
)
