local test = import 'test.libsonnet';

test.mergeOptional(
  {
    model: test.model(),
    name: 'test-evaluation-job-git-bad-ref',
    benchmarks: [
      test.gitArcEasyBenchmark({}, {
        ref: test.env('TEST_DATA_GIT_BAD_REF', 'this-ref-does-not-exist-evalhub-fvt'),
      }),
    ],
    tags: ['environment', 'git', 'negative'],
  },
  test.experiment('my-test-experiment'),
)
