local test = import 'test.libsonnet';

{
  model: test.model(),
  name: 'test-evaluation-job-git-resolved-sha-readonly',
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
        git: {
          url: test.env('TEST_DATA_GIT_URL', 'https://github.com/eval-hub/eval-hub'),
          ref: test.env('TEST_DATA_GIT_REF', 'main'),
          sub_path: test.env('TEST_DATA_GIT_SUB_PATH', 'tests/git-testdata'),
        },
      },
    },
  ],
}
