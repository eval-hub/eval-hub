local test = import 'test.libsonnet';

{
  name: 'test-evaluation-job-queue-collection',
  collection: {
    id: 'toxicity-and-ethical-principles',
    benchmarks: std.map(
      function(id)
        test.benchmark(id, 'lm_evaluation_harness', test.oobCollectionParameterOverrides(test.defaultOobNumExamples())) + {
          hardware_config: {
            queue: test.queueConfig(),
          },
        },
      test.toxicityAndEthicalPrinciplesBenchmarkIds(),
    ),
  },
  model: test.model(),
}
