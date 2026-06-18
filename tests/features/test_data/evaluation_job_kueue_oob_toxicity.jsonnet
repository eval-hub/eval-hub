local test = import 'test.libsonnet';

{
  name: 'test-evaluation-job-queue-collection',
  queue: {
    kind: 'kueue',
    name: test.env('QUEUE_NAME', 'user-queue'),
  },
  collection: {
    id: 'toxicity-and-ethical-principles',
    benchmarks: [
      test.benchmark('toxigen', 'lm_evaluation_harness', { limit: 4 }),
      test.benchmark('truthfulqa_mc1', 'lm_evaluation_harness', { limit: 3 }),
      test.benchmark('bigbench_hhh_alignment_multiple_choice', 'lm_evaluation_harness', { limit: 1 }),
    ],
  },
  model: test.model(),
}
