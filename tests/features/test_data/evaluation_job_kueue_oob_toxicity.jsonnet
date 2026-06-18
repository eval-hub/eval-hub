local test = import 'test.libsonnet';

{
  name: 'test-evaluation-job-queue-collection',
  queue: {
    kind: 'kueue',
    name: test.env('QUEUE_NAME', 'user-queue'),
  },
  collection: {
    id: 'toxicity-and-ethical-principles',
    benchmarks: std.map(
      function(id) test.benchmark(id, 'lm_evaluation_harness', test.oobCollectionParameterOverrides(test.defaultOobNumExamples())),
      test.toxicityAndEthicalPrinciplesBenchmarkIds(),
    ),
  },
  model: test.model(),
}
