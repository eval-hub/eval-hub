local test = import 'test.libsonnet';

{
  name: 'mcp_cluster_test',
  description: 'Cluster test via MCP',
  model: test.model(),
  benchmarks: [
    test.benchmark('arc_easy', 'lm_evaluation_harness', {
      num_fewshot: 0,
      num_examples: 10,
    }),
  ],
}
