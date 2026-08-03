local test = import 'test.libsonnet';

// MCP submit_evaluation expects experiment.tags as a string map (not HTTP API [{key,value}] arrays).
// Omit tags here; name alone is enough for the FVT scenario.
{
  name: 'mcp_cluster_mlflow_test',
  description: 'Cluster MLflow tracking test via MCP',
  model: test.model(),
  benchmarks: [
    test.benchmark('arc_easy', 'lm_evaluation_harness', {
      num_fewshot: 0,
      num_examples: 5,
    }),
  ],
  experiment: {
    name: 'mcp_cluster_mlflow_experiment',
  },
}
