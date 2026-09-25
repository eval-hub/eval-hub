local test = import 'test.libsonnet';

test.mergeOptional(
  {
    model: test.model(),
    name: 'test-evaluation-job-hf-pinned-revision',
    benchmarks: [
      test.hfArcEasyBenchmark({}, {
        repo_id: test.env('TEST_DATA_HF_REPO_ID', 'eval-hub-test/evalhub-offline-testdata'),
        revision: test.env(
          'TEST_DATA_HF_PINNED_SHA',
          '96edc9dc18c4421a32976c06ffc78955fb4507bd',
        ),
      }),
    ],
    tags: ['environment', 'hf'],
  },
  test.experiment('my-test-experiment'),
)
