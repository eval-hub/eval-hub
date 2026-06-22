local test = import 'test.libsonnet';

test.mergeOptional(
  test.oobCollectionRefJobWithLimit(
    'test-evaluation-job',
    'leaderboard-v2',
    test.leaderboardV2BenchmarkIds(),
  ),
  test.mergeOptional(
    test.experiment('oob-collection-experiment'),
    { tags: ['environment'] },
  ),
)
