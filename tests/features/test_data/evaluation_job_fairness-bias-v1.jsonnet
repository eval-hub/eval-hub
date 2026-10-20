local test = import 'test.libsonnet';

test.oobCollectionRefJobWithBenchmarks(
  'test-evaluation-job-oob-collection',
  'fairness-bias-v1',
  [
    test.benchmark('winogender', 'lm_evaluation_harness', { num_examples: 1 }),
    test.benchmark('crows_pairs_english', 'lm_evaluation_harness', { num_examples: 1 }),
    test.benchmark('bbq', 'lm_evaluation_harness', { num_examples: 1 }),
  ],
)
