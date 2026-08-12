local test = import 'test.libsonnet';

test.mergeOptional(
  {
    model: test.model(),
    name: 'test-evaluation-job-git-tag',
    benchmarks: [
      test.gitArcEasyBenchmark({}, {
        // Set TEST_DATA_GIT_TAG_REF to a real tag on eval-hub that contains tests/git-testdata.
        ref: test.env('TEST_DATA_GIT_TAG_REF', test.env('TEST_DATA_GIT_REF', 'main')),
      }),
    ],
    tags: ['environment', 'git', 'tag'],
  },
  test.experiment('my-test-experiment'),
)
