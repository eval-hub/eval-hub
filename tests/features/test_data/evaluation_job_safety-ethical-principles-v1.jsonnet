local test = import 'test.libsonnet';

test.oobCollectionRefJobWithBenchmarks(
  'test-evaluation-job-oob-collection',
  'safety-ethical-principles-v1',
  [
    test.benchmark('toxigen', 'lm_evaluation_harness', { num_examples: 1 }),
    test.benchmark('ethics_cm', 'lm_evaluation_harness', { num_examples: 1 }),
    test.benchmark('bigbench_hhh_alignment_multiple_choice', 'lm_evaluation_harness', { num_examples: 1 }),
  ],
)
