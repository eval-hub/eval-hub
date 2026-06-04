local test = import 'test.libsonnet';

{
  name: 'job-collection-override',
  description: 'Override parameter',
  category: 'test',
  benchmarks: [
    test.benchmark('arc_easy', 'lm_evaluation_harness', {
      num_examples: 3,
    }),
  ],
}
