local test = import 'test.libsonnet';

test.mergeOptional(
  {
    model: test.model(),
    name: 'test-evaluation-job-git-sha',
    benchmarks: [
      test.gitArcEasyBenchmark({}, {
        // Defaults to TEST_DATA_GIT_REF/main (branch-like), same as the main happy path.
        // To exercise commit-SHA checkout, set TEST_DATA_GIT_SHA_REF to a real hex SHA that
        // contains tests/git-testdata (optionally override URL with TEST_DATA_GIT_SHA_URL).
        url: test.env('TEST_DATA_GIT_SHA_URL', test.env('TEST_DATA_GIT_URL', 'https://github.com/eval-hub/eval-hub')),
        ref: test.env('TEST_DATA_GIT_SHA_REF', test.env('TEST_DATA_GIT_REF', 'main')),
      }),
    ],
    tags: ['environment', 'git', 'sha'],
  },
  test.experiment('my-test-experiment'),
)
