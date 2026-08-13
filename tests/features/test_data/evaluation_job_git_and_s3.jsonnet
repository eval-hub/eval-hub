local test = import 'test.libsonnet';

{
  model: test.model(),
  name: 'test-evaluation-job-git-and-s3',
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
        s3: {
          bucket: test.env('TEST_DATA_S3_BUCKET', 'mlpipeline'),
          key: test.env('TEST_DATA_S3_KEY', 'offline'),
          secret_ref: test.env('TEST_DATA_S3_SECRET_REF', 'minio-test'),
        },
      },
    },
  ],
}
