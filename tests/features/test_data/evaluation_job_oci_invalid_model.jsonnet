local test = import 'jsonnet/test.libsonnet';

{
  name: 'oci-invalid-model',
  description: 'testing oci export when job fails',
  tags: ['oci', 'fail'],
  // Override model URL to force failure, but keep auth structure from test.model()
  model: test.model() + {
    url: 'http://nonexistent-model-endpoint.invalid',
    name: 'invalid-model',
  },
  benchmarks: [
    test.benchmark('arc_easy', 'lm_evaluation_harness', {
      num_examples: 5,
    }),
  ],
  exports: {
    oci: {
      coordinates: {
        oci_host: test.env('OCI_REGISTRY'),
        oci_repository: test.env('OCI_REPOSITORY'),
        oci_tag: test.env('OCI_TAG_FAILED', 'failed-job-test'),
      },
      k8s: {
        connection: test.env('OCI_SECRET_NAME'),
      },
    },
  },
}
