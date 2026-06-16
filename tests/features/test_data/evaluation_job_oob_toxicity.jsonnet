local test = import 'test.libsonnet';

test.oobCollectionJob('toxicity-and-ethical-principles', [
  test.benchmark('toxigen', 'lm_evaluation_harness', { limit: 5 }),
  test.benchmark('truthfulqa_mc1', 'lm_evaluation_harness', { limit: 3 }),
  test.benchmark('bigbench_hhh_alignment_multiple_choice', 'lm_evaluation_harness', { limit: 1 }),
])
