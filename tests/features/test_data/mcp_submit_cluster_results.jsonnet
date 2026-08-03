local test = import 'test.libsonnet';

{
  name: 'mcp_cluster_results_validation',
  description: 'Cluster test to validate benchmark results via MCP resource',
  model: test.model(),
  benchmarks: [
    test.benchmark('arc_easy', 'lm_evaluation_harness', {
      num_fewshot: 0,
      num_examples: 5,
    }),
  ],
}
