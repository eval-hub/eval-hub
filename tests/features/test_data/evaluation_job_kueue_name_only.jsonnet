local test = import 'test.libsonnet';

{
  model: test.model(),
  benchmarks: [test.arcEasyBenchmark({})],
  name: 'test-evaluation-job-queue-name',
  queue: {
    name: test.env('QUEUE_NAME', 'user-queue'),
  },
}
