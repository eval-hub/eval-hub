local test = import 'test.libsonnet';
local collectionId = test.value('collection_id');
// The embedding connection comes from embedding_url/embedding_api-key in the
// Secret referenced by MODEL_AUTH_SECRET_REF; the adapter uses sidecar refs.
local requiredEmbeddingEnv(name) =
  local value = std.stripChars(test.env(name), ' \t\r\n');
  assert value != '' : 'Set ' + name + ' to the embedding model ID before running Ragas tests';
  value;
local parameters = {
  num_examples: 1,
  embedding_model: requiredEmbeddingEnv('RAGAS_EMBEDDING_MODEL'),
};
local connectedTestDataRef = if test.disconnected() then {} else {
  test_data_ref: {
    s3: {
      bucket: test.env('TEST_DATA_S3_BUCKET', 'mlpipeline'),
      key: 'datasets/ragas/dataset.jsonl',
      secret_ref: 's3-ragas-credentials',
    },
  },
};

test.mergeOptional(
  test.mergeOptional(
    {
      model: test.model(),
      name: 'test-evaluation-job-for-ragas-benchmark',
    } + if collectionId == '' then {
      benchmarks: [
        test.benchmark('ragas_rag_default', 'ragas', parameters) + connectedTestDataRef,
        test.benchmark('ragas_rag_full', 'ragas', parameters) + connectedTestDataRef,
      ],
      tags: ['benchmark-providers', 'ragas'],
    } else {},
    if collectionId != '' then test.collection() else null,
  ),
  test.experiment('my-test-experiment'),
)
