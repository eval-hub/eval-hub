local test = import 'test.libsonnet';

test.oobCollectionRefJobWithBenchmarks(
  'test-evaluation-job-oob-collection',
  'software-v1',
  [
    test.benchmark('lcb:codegeneration_v6', 'lighteval', { num_examples: 1 }),
    test.benchmark('inspect/bigcodebench', 'inspect', { num_examples: 1 }),
    test.benchmark('cruxeval_output', 'lm_evaluation_harness', { num_examples: 1 }),
  ],
)
