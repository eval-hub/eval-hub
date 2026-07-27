local test = import 'test.libsonnet';

{
  name: 'pre-upgrade-job1',
  model: test.model(),
  benchmarks: [
    {
      id: 'arc_easy',
      provider_id: 'lm_evaluation_harness',
      parameters: {
        num_examples: 5,
        num_few_shot: 0,
        tokenizer: 'ibm-granite/granite-3.3-2b-instruct',
      },
    },
  ],
}
