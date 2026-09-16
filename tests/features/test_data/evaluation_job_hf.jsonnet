local test = import 'test.libsonnet';

test.mergeOptional(
  {
    model: test.model(),
    name: 'test-evaluation-job-hf',
    // Two HF benchmarks in one job: full repo (arc_easy) + nested sub_path (truthfulqa_mc1).
    // Covers branch revision (TEST_DATA_HF_REVISION), optional pinned SHA on arc_easy
    // (TEST_DATA_HF_SHA_REVISION), and resolved_sha population for each benchmark.
    benchmarks: [
      test.hfArcEasyBenchmark({}, {
        revision: test.env('TEST_DATA_HF_SHA_REVISION', test.env('TEST_DATA_HF_REVISION', 'main')),
      }),
      test.hfTruthfulqaMc1Benchmark({}, {
        sub_path: test.env('TEST_DATA_HF_NESTED_SUB_PATH', 'staging_sub_path'),
      }),
    ],
    tags: ['environment', 'hf'],
  },
  test.experiment('my-test-experiment'),
)
