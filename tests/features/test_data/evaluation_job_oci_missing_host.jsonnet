local test = import 'jsonnet/test.libsonnet';

{
  name: 'oci-validation-error',
  model: test.model(),
  benchmarks: [
    test.benchmark('arc_easy', 'lm_evaluation_harness', {}),
  ],
  exports: {
    oci: {
      coordinates: {
        // Missing oci_host - should cause validation error
        oci_repository: 'evalhub/test-results',
      },
      k8s: {
        connection: test.env('OCI_SECRET_NAME'),
      },
    },
  },
}
