local test = import 'test.libsonnet';

{
  model: test.model(),
  benchmarks: [test.arcEasyBenchmark({})],
  name: 'test-evaluation-job-queue-tags-criteria',
  queue: {
    kind: 'kueue',
    name: test.env('QUEUE_NAME', 'user-queue'),
  },
  tags: [
    'integration-test',
    'kueue-enabled',
  ],
  pass_criteria: {
    threshold: 0.8,
  },
}
