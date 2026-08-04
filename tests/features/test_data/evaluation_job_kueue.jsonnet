local test = import 'test.libsonnet';

{
  model: test.model(),
  benchmarks: [
    test.arcEasyBenchmark({}) + {
      hardware_config: {
        queue: test.queueConfig(),
      },
    },
  ],
  name: 'test-evaluation-job-queue',
}
