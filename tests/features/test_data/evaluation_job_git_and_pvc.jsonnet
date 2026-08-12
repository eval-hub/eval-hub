local test = import 'test.libsonnet';

{
  model: test.model(),
  name: 'test-evaluation-job-git-and-pvc',
  benchmarks: [
    {
      id: 'arc_easy',
      provider_id: 'lm_evaluation_harness',
      parameters: {
        tokenizer: '/test_data/tokenizer',
        num_examples: 10,
      },
      test_data_ref: {
        git: {
          url: test.env('TEST_DATA_GIT_URL', 'https://github.com/eval-hub/eval-hub'),
          ref: test.env('TEST_DATA_GIT_REF', 'main'),
          sub_path: test.env('TEST_DATA_GIT_SUB_PATH', 'tests/git-testdata'),
        },
        pvc: {
          claim_name: test.env('TEST_DATA_PVC_CLAIM_NAME', 'evalhub-offline-test-data'),
          sub_path: test.env('TEST_DATA_PVC_SUB_PATH', 'staging'),
        },
      },
    },
  ],
}
