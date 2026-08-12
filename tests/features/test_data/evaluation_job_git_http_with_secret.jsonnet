local test = import 'test.libsonnet';

{
  model: test.model(),
  name: 'test-evaluation-job-git-http-with-secret',
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
          url: test.env('TEST_DATA_GIT_HTTP_URL', 'http://git.example.com/repo.git'),
          ref: 'main',
          secret_ref: test.env('TEST_DATA_GIT_SECRET_REF', 'github-creds'),
        },
      },
    },
  ],
}
