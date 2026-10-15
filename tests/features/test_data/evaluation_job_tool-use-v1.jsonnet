local test = import 'test.libsonnet';

test.oobCollectionRefJobWithBenchmarks(
  'test-evaluation-job-oob-collection',
  'tool-use-v1',
  [
    test.benchmark('inspect/bfcl', 'inspect', { num_examples: 1 }),
    test.benchmark('inspect/bfcl', 'inspect', { num_examples: 1 }),
  ],
)
