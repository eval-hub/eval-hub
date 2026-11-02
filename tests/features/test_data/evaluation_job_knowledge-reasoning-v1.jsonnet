local test = import 'test.libsonnet';

test.oobCollectionRefJobWithBenchmarks(
  'test-evaluation-job-oob-collection',
  'knowledge-reasoning-v1',
  [
    test.benchmark('gpqa:diamond', 'lighteval', { num_examples: 1, num_samples: 1 }),
    test.benchmark('inspect/hle', 'inspect', { num_examples: 1, judge_model: test.model().name }),
    test.benchmark('math_500', 'lighteval', { num_examples: 1, num_samples: 1 }),
  ],
)
