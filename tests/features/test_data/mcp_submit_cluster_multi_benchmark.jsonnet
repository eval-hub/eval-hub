local test = import 'test.libsonnet';

{
  name: 'mcp_cluster_multi_benchmark_test',
  description: 'Cluster multi-benchmark test via MCP',
  model: test.model(),
  benchmarks: [
    test.benchmark('arc_easy', 'lm_evaluation_harness', {
      num_fewshot: 0,
      num_examples: 3,
    }),
    test.benchmark('truthfulqa_mc1', 'lm_evaluation_harness', {
      num_fewshot: 0,
      num_examples: 3,
    }),
  ],
}
