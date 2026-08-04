local test = import 'test.libsonnet';

{
  model: test.model(),
  benchmarks: [
    test.arcEasyBenchmark({}) + {
      hardware_config: {
        queue: {
          kind: 'kueue',
          name: '  user-queue  ',
        },
      },
    },
  ],
  name: 'test-evaluation-job-queue',
}
