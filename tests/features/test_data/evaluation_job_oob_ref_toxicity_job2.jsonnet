local test = import 'test.libsonnet';

test.oobCollectionRefJobWithLimit(
  'multiple-job-different-collection-2',
  'toxicity-and-ethical-principles',
  test.toxicityAndEthicalPrinciplesBenchmarkIds(),
)
