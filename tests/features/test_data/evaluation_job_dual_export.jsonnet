local test = import 'jsonnet/test.libsonnet';

{
  name: 'dual_export_oci_mlflow',
  description: 'testing both OCI and MLflow exports',
  tags: ['eval_card', 'oci', 'mlflow'],
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
        oci_tag: test.env('OCI_TAG_DUAL', 'dual-export-test'),
      },
      k8s: {
        connection: test.env('OCI_SECRET_NAME'),
      },
    },
  },
  experiment: {
    name: 'dual_export_experiment',
  },
}
