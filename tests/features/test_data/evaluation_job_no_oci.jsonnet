local test = import 'jsonnet/test.libsonnet';

{
  name: 'no-oci-export',
  description: 'Job without OCI export configuration',
  model: test.model(),
  benchmarks: [
    test.benchmark('arc_easy', 'lm_evaluation_harness', {
      num_examples: 5,
    }),
  ],
}
