local test = import 'test.libsonnet';

{
  model: test.model(),
  name: 'test-evaluation-job-pvc-and-s3',
  benchmarks: [
    {
      id: 'arc_easy',
      provider_id: 'lm_evaluation_harness',
      parameters: {
        tokenizer: '/test_data/tokenizer',
        num_examples: 10,
      },
      test_data_ref: {
        pvc: {
          claim_name: test.env('TEST_DATA_PVC_CLAIM_NAME', 'evalhub-offline-test-data'),
          sub_path: test.env('TEST_DATA_PVC_SUB_PATH', 'staging'),
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
