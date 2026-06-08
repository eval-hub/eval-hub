local test = import 'test.libsonnet';

{
  name: 'automation_shared_experiment_job_2',
  model: test.model(),
  queue: {
    kind: 'kueue',
    name: test.env('QUEUE_NAME', 'user-queue'),
  },
  benchmarks: [
    test.benchmark('arc_easy', 'lm_evaluation_harness', { num_examples: 3 }),
  ],
}
