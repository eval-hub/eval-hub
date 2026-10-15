local test = import 'test.libsonnet';

test.oobCollectionRefJobWithBenchmarks(
  'test-evaluation-job-oob-collection',
  'instruction-output-v1',
  [
    test.benchmark('ifbench', 'ifbench', { num_examples: 1 }),
    test.benchmark('jsonschema_bench_easy', 'lm_evaluation_harness', { num_examples: 1 }),
    test.benchmark('followbench', 'followbench', { num_examples: 1 }),
  ],
)
