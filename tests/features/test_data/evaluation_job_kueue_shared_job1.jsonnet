local test = import 'test.libsonnet';

{
  name: 'automation_shared_experiment_job_1',
  queue: {
    kind: 'kueue',
    name: test.env('QUEUE_NAME', 'user-queue'),
  },
  model: test.model(),
  benchmarks: [
    test.benchmark('arc_easy', 'lm_evaluation_harness', { num_examples: 3 }),
  ],
}
