local test = import 'test.libsonnet';

test.oobCollectionRefJobWithLimit(
  'multiple-job-different-collection-3',
  'leaderboard-v2',
  test.leaderboardV2BenchmarkIds(),
)
