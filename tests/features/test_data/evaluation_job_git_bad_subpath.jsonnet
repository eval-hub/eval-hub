local test = import 'test.libsonnet';

test.mergeOptional(
  {
    model: test.model(),
    name: 'test-evaluation-job-git-bad-subpath',
    benchmarks: [
      test.gitArcEasyBenchmark({}, {
        sub_path: test.env('TEST_DATA_GIT_BAD_SUB_PATH', 'this-path-does-not-exist-evalhub-fvt'),
      }),
    ],
    tags: ['environment', 'git', 'negative'],
  },
  test.experiment('my-test-experiment'),
)
