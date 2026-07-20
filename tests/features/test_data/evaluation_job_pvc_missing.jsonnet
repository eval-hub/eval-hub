local test = import 'test.libsonnet';

// Non-existent PVC — job should fail after operator scheduling grace (~2m).
test.mergeOptional(
  test.mergeOptional(
    {
      model: test.model(),
      name: 'test-evaluation-job-pvc-missing',
      benchmarks: [
        {
          id: 'arc_easy',
          provider_id: 'lm_evaluation_harness',
          parameters: {
            tokenizer: '/test_data/tokenizer',
            num_examples: 5,
          },
          test_data_ref: {
            pvc: {
              claim_name: test.env('TEST_DATA_PVC_MISSING_CLAIM_NAME', 'evalhub-offline-test-data-does-not-exist'),
            },
          },
        },
      ],
      tags: ['environment', 'pvc', 'negative'],
    },
    test.experiment('my-test-experiment'),
  ),
  test.queue(),
)
