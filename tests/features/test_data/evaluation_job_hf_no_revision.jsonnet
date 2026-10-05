local test = import 'test.libsonnet';

test.mergeOptional(
  {
    model: test.model(),
    name: 'test-evaluation-job-hf-revision-omitted',
    benchmarks: [
       std.prune(
          test.hfArcEasyBenchmark({}, {
            revision: null,
          })
        ),
    ],
    tags: ['environment', 'hf'],
  },
  test.experiment('my-test-experiment'),
)
