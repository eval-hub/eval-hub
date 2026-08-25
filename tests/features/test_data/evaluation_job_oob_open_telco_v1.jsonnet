local test = import 'test.libsonnet';

test.oobCollectionRefJobWithBenchmarks(
  'test-evaluation-job-oob-collection',
  'open-telco-v1',
  [
    test.benchmark('inspect/telemath', 'inspect', { num_examples: 5 }),
    test.benchmark('inspect/teleqna', 'inspect', { num_examples: 5 }),
    test.benchmark('inspect/telelogs', 'inspect', { num_examples: 5 }),
    test.benchmark('inspect/3gpp-tsg', 'inspect', { num_examples: 5 }),
  ],
)
