local test = import 'test.libsonnet';

{
  name: 'evalcard_multibenchmark_test',
  description: 'Job for card testing with multiple benchmarks',
  tags: ['evalcard', 'multi'],
  model: test.model(),
  benchmarks: [
    test.benchmark('arc_easy', 'lm_evaluation_harness', { num_examples: 5 }),
    test.benchmark('arc_easy', 'lm_evaluation_harness', { num_examples: 3 }),
  ],
  pass_criteria: {
    threshold: 0.5,
  },
  experiment: {
    name: 'evalcard_multi_experiment',
  },
}
