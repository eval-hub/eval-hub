local test = import 'test.libsonnet';

{
  name: 'evalcard_benchmark_test',
  description: 'Job for card testing with benchmarks',
  tags: ['evalcard', 'test'],
  model: test.model(),
  benchmarks: [
    test.benchmark('arc_easy', 'lm_evaluation_harness', {
      num_examples: 5,
    }) + {
      weight: 0.6,
      primary_score: {
        metric: 'accuracy',
        aggregation: 'mean',
      },
      pass_criteria: {
        threshold: 0.3,
      },
    },
  ],
  pass_criteria: {
    threshold: 0.3,
  },
  experiment: {
    name: 'evalcard_test_experiment',
  },
}
