local test = import 'test.libsonnet';

test.oobCollectionRefJobWithBenchmarks(
  'test-evaluation-job-oob-collection',
  'open-telco-v1',
  [
    test.benchmark('telemath', 'inspect', { num_examples: 5 }),
    test.benchmark('teleqna', 'inspect', { num_examples: 5 }),
    test.benchmark('telelogs', 'inspect', { num_examples: 5 }),
    test.benchmark('3gpp-tsg', 'inspect', { num_examples: 5 }),
  ],
)
