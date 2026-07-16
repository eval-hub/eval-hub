local test = import 'test.libsonnet';

test.mergeOptional(
  test.mergeOptional(
    {
      model: test.model(),
      name: 'test-evaluation-job-pvc',
      benchmarks: [test.pvcArcEasyBenchmark()],
      tags: ['environment', 'pvc'],
    },
    test.experiment('my-test-experiment'),
  ),
  test.queue(),
)
