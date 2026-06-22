local test = import 'test.libsonnet';

test.oobCollectionRefJobWithLimit(
  'multiple-job-same-collection-1',
  'safety-and-fairness-v1',
  test.safetyAndFairnessV1BenchmarkIds(),
)
