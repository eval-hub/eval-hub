local test = import 'test.libsonnet';

test.mergeOptional(
  {
    name: 'automation_test_evaluation_multiple_benchmark_job',
    description: 'This is a test job for automation using multiple benchmarks',
    tags: [
      'evalhub',
      'test',
    ],
    model: test.model(),
    benchmarks: [
      test.benchmark('arc_easy', 'lm_evaluation_harness', { num_examples: 5 }),
      test.benchmark('arc_easy', 'lm_evaluation_harness', { num_examples: 5 }),
    ],
    pass_criteria: {
      threshold: 0.5,
    },
    custom: {},
  },
  test.experiment('automation_test_experiment'),
)
