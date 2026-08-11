local test = import 'test.libsonnet';

test.mergeOptional(
  {
    model: test.model(),
    name: 'test-evaluation-job-git',
    // Two git benchmarks (default + nested sub_path) exercises per-benchmark clone/index
    // threading in one job; also covers blank→populated resolved_sha for each.
    benchmarks: [
      test.gitArcEasyBenchmark(),
      test.gitTruthfulqaMc1Benchmark({}, {
        sub_path: test.env('TEST_DATA_GIT_NESTED_SUB_PATH', 'tests/git-testdata/staging_sub_path'),
      }),
    ],
    tags: ['environment', 'git'],
  },
  test.experiment('my-test-experiment'),
)
