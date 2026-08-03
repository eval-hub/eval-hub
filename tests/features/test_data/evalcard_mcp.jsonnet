local test = import 'test.libsonnet';

test.evalCardArcEasyJob(
  'evalcard_mcp_test',
  'Job for MCP card testing',
  ['evalcard', 'mcp'],
  'evalcard_mcp_experiment',
)
