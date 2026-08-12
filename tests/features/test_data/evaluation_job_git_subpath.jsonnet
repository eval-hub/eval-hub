local test = import 'test.libsonnet';

test.mergeOptional(
  {
    model: test.model(),
    name: 'test-evaluation-job-git-subpath',
    benchmarks: [
      // Different benchmark than the default git arc_easy path: truthfulqa_mc1 offline
      // data lives only under tests/git-testdata/staging_sub_path.
      test.gitTruthfulqaMc1Benchmark({}, {
        sub_path: test.env('TEST_DATA_GIT_NESTED_SUB_PATH', 'tests/git-testdata/staging_sub_path'),
      }),
    ],
    tags: ['environment', 'git', 'subpath'],
  },
  test.experiment('my-test-experiment'),
)
