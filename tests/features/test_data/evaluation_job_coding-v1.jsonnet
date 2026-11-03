local test = import 'test.libsonnet';

test.oobCollectionRefJobWithBenchmarks(
  'test-evaluation-job-oob-collection',
  'coding-v1',
  [
    test.benchmark('lcb:codegeneration_v6', 'lighteval', { num_examples: 1 }),
  ],
)
