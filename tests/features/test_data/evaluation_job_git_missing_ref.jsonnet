local test = import 'test.libsonnet';

{
  model: test.model(),
  name: 'test-evaluation-job-git-missing-ref',
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
        },
      },
    },
  ],
}
