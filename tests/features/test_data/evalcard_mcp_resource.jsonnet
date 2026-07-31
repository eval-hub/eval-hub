local test = import 'test.libsonnet';

test.evalCardArcEasyJob(
  'evalcard_mcp_resource_test',
  'Test MCP resource includes mlflow_run_id',
  ['evalcard', 'mcp', 'resource'],
  'evalcard_mcp_resource_experiment',
)
