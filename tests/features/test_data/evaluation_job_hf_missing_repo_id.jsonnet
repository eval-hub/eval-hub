local test = import 'test.libsonnet';

{
  model: test.model(),
  name: 'test-evaluation-job-hf-missing-repo-id',
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
          revision: 'main',
        },
      },
    },
  ],
}
