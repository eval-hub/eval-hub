local test = import 'test.libsonnet';
local collectionId = test.value('collection_id');
// Chat-only model endpoints cannot serve the embedding metrics in either suite.
local requiredEmbeddingEnv(name) =
  local value = std.stripChars(test.env(name), ' \t\r\n');
  assert value != '' : 'Set ' + name + ' to an embedding-capable model/endpoint before running Ragas tests';
  value;
local parameters = {
  num_examples: 1,
  embedding_model: requiredEmbeddingEnv('RAGAS_EMBEDDING_MODEL'),
  embedding_url: requiredEmbeddingEnv('RAGAS_EMBEDDING_URL'),
};

test.mergeOptional(
  test.mergeOptional(
    {
      model: test.model(),
      name: 'test-evaluation-job-for-ragas-benchmark',
    } + if collectionId == '' then {
      benchmarks: [
        test.benchmark('ragas_rag_default', 'ragas', parameters) + {
          test_data_ref: {
            s3: {
              bucket: 'ai-eng-canada',
              key: 'datasets/ragas/dataset.jsonl',
              secret_ref: 's3-ragas-credentials',
            },
          },
        },
        test.benchmark('ragas_rag_full', 'ragas', parameters) + {
          test_data_ref: {
            s3: {
              bucket: 'ai-eng-canada',
              key: 'datasets/ragas/dataset.jsonl',
              secret_ref: 's3-ragas-credentials',
            },
          },
        },
      ],
      tags: ['benchmark-providers', 'ragas'],
    } else {},
    if collectionId != '' then test.collection() else null,
  ),
  test.experiment('my-test-experiment'),
)
