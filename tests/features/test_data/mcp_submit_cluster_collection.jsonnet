local test = import 'test.libsonnet';

test.oobCollectionRefJobWithLimit(
  'mcp_cluster_collection_test',
  'toxicity-and-ethical-principles',
  test.toxicityAndEthicalPrinciplesBenchmarkIds(),
) + {
  description: 'Cluster collection test via MCP',
}
