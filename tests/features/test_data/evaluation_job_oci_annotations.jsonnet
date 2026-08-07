local test = import 'jsonnet/test.libsonnet';

{
  name: 'oci-custom-annotations',
  description: 'Testing OCI export with custom annotations',
  tags: ['annotations', 'metadata'],
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
        oci_tag: test.env('OCI_TAG_ANNOTATIONS', 'annotations-test'),
        annotations: {
          team: 'ml-platform',
          environment: 'test',
          'cost-center': 'research',
        },
      },
      k8s: {
        connection: test.env('OCI_SECRET_NAME'),
      },
    },
  },
}
