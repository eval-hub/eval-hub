local test = import 'test.libsonnet';

test.oobCollectionRefJobWithBenchmarks(
  'test-evaluation-job-oob-collection',
  'trustworthiness-v1',
  [
    test.benchmark('inspect/cyberseceval-2-pi', 'inspect', { num_examples: 1 }),
    test.benchmark('truthfulqa_mc1', 'lm_evaluation_harness', { num_examples: 1 }),
  ],
)
