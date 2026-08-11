local test = import 'jsonnet/test.libsonnet';

{
  name: 'oci-shared-repo-job2',
  model: test.model(),
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
        oci_tag: test.env('OCI_TAG_SHARED2', 'shared-repo-v2'),
      },
      k8s: {
        connection: test.env('OCI_SECRET_NAME'),
      },
    },
  },
}
