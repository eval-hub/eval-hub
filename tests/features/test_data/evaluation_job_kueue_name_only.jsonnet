local test = import 'test.libsonnet';

{
  model: test.model(),
  benchmarks: [
    test.arcEasyBenchmark({}) + {
      hardware_config: {
        queue: {
          name: test.env('QUEUE_NAME', 'user-queue'),
        },
      },
    },
  ],
  name: 'test-evaluation-job-queue-name',
}
