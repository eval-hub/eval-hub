local test = import 'test.libsonnet';

{
  name: 'test-multi-benchmarks-collection',
  description: 'Collection of multiple benchmarks for FVT',
  category: 'test',
  benchmarks: [
    test.benchmark('arc_easy', 'lm_evaluation_harness', {
      limit: 5,
      num_examples: 10,
    }),
    test.benchmark('arc_easy', 'lm_evaluation_harness', {
      num_examples: 15,
      num_fewshot: 3,
      limit: 5,
    }),
  ],
}
