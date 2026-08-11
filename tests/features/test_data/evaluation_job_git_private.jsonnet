local test = import 'test.libsonnet';

test.mergeOptional(
  {
    model: test.model(),
    name: 'test-evaluation-job-git-private',
    benchmarks: [
      test.gitArcEasyBenchmark({}, {
        // Override TEST_DATA_GIT_PRIVATE_URL to a private fork/mirror that has the same layout.
        url: test.env('TEST_DATA_GIT_PRIVATE_URL', 'https://github.com/eval-hub/eval-hub'),
        ref: test.env('TEST_DATA_GIT_PRIVATE_REF', test.env('TEST_DATA_GIT_REF', 'main')),
        secret_ref: test.env('TEST_DATA_GIT_SECRET_REF', 'github-creds'),
        sub_path: test.env('TEST_DATA_GIT_SUB_PATH', 'tests/git-testdata'),
      }),
    ],
    tags: ['environment', 'git', 'private'],
  },
  test.experiment('my-test-experiment'),
)
