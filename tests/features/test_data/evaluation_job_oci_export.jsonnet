local test = import 'jsonnet/test.libsonnet';

{
  name: 'eval_card_with_oci',
  description: 'testing eval card exported to OCI registry',
  tags: ['eval_card', 'oci'],
  model: test.model(),
  benchmarks: [
    test.benchmark('arc_easy', 'lm_evaluation_harness', {
      num_examples: 5,
    }),
  ],
  exports: {
    oci: {
      coordinates: {
        oci_host: test.env('OCI_REGISTRY', 'http://registry.evalhub.svc.cluster.local:5000'),
        oci_repository: test.env('OCI_REPOSITORY', 'evalhub/test-results'),
        oci_tag: test.env('OCI_TAG', 'test-v1'),
      },
      k8s: {
        connection: test.env('OCI_SECRET_NAME', 'oci-registry-credentials'),
      },
    },
  },
}
