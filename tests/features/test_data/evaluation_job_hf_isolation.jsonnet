local test = import 'test.libsonnet';

test.mergeOptional(
  {
    model: test.model(),
    name: 'test-evaluation-job-hf-credential-isolation',
    benchmarks: [
      test.hfArcEasyBenchmark({}, {
        secret_ref: test.env('HF_SECRET_REF', 'hftoken'),
      }),
    ],
    tags: ['environment', 'hf', 'security'],
  },
  test.experiment('my-test-experiment'),
)
