local test = import 'test.libsonnet';

{
  model: test.model(),
  name: 'test-evaluation-job-hf-resolved-sha-readonly',
  benchmarks: [
    {
      id: 'arc_easy',
      provider_id: 'lm_evaluation_harness',
      parameters: {
        tokenizer: '/test_data/tokenizer',
        num_examples: 10,
      },
      test_data_ref: {
        resolved_sha: 'deadbeefdeadbeefdeadbeefdeadbeef00000000',
        hf: {
          repo_id: test.env('TEST_DATA_HF_REPO_ID', 'eval-hub-test/evalhub-offline-testdata'),
          revision: test.env('TEST_DATA_HF_REVISION', 'main'),
        },
      },
    },
  ],
}
