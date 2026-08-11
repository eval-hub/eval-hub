local test = import 'test.libsonnet';

{
  model: test.model(),
  name: 'test-evaluation-job-git-blocked-host',
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
          url: test.env('TEST_DATA_GIT_BLOCKED_URL', 'http://192.168.1.1/repo.git'),
          ref: 'main',
        },
      },
    },
  ],
}
