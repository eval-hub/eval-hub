local test = import 'test.libsonnet';

test.mergeOptional(
  {
    model: test.model(),
    name: 'test-evaluation-job-hf-runtime-failures',
    // One job exercises two distinct init failure paths (invalid repo vs invalid revision).
    benchmarks: [
      test.hfArcEasyBenchmark({}, {
        repo_id: test.env('TEST_DATA_HF_BAD_REPO_ID', 'eval-hub-test/invalid-db'),
      }),
      test.hfTruthfulqaMc1Benchmark({}, {
        revision: test.env('TEST_DATA_HF_BAD_REVISION', 'this-revision-does-not-exist-evalhub-fvt'),
        sub_path: test.env('TEST_DATA_HF_NESTED_SUB_PATH', 'staging_sub_path'),
      }),
    ],
    tags: ['environment', 'hf', 'negative'],
  },
  test.experiment('my-test-experiment'),
)
