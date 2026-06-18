local test = import 'test.libsonnet';

test.oobCollectionRefJobWithLimit(
  'test-evaluation-job-oob-collection',
  'toxicity-and-ethical-principles',
  test.toxicityAndEthicalPrinciplesBenchmarkIds(),
)
