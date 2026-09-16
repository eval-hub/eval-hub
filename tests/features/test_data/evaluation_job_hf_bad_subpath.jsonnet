local test = import 'test.libsonnet';

test.mergeOptional(
  {
    model: test.model(),
    name: 'test-evaluation-job-hf-bad-subpath',
    benchmarks: [
      test.hfArcEasyBenchmark({}, {
        sub_path: test.env('TEST_DATA_HF_BAD_SUB_PATH', 'this-path-does-not-exist-evalhub-fvt'),
      }),
    ],
    tags: ['environment', 'hf', 'negative'],
  },
  test.experiment('my-test-experiment'),
)
