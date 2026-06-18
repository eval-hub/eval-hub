local test = import 'test.libsonnet';

test.mergeOptional(
  test.oobCollectionRefJob('test-evaluation-job', 'leaderboard-v2'),
  test.mergeOptional(
    test.experiment('oob-collection-experiment'),
    { tags: ['environment'] },
  ),
)
