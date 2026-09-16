local test = import 'test.libsonnet';

{
  model: test.model(),
  name: 'test-evaluation-job-hf-and-s3',
  benchmarks: [
    {
      id: 'arc_easy',
      provider_id: 'lm_evaluation_harness',
      parameters: {
        tokenizer: '/test_data/tokenizer',
        num_examples: 10,
      },
      test_data_ref: {
        hf: {
          repo_id: test.env('TEST_DATA_HF_REPO_ID', 'eval-hub-test/evalhub-offline-testdata'),
          revision: test.env('TEST_DATA_HF_REVISION', 'main'),
        },
        s3: {
          bucket: test.env('TEST_DATA_S3_BUCKET', 'mlpipeline'),
          key: test.env('TEST_DATA_S3_KEY', 'offline'),
          secret_ref: test.env('TEST_DATA_S3_SECRET_REF', 'minio-test'),
        },
      },
    },
  ],
}
