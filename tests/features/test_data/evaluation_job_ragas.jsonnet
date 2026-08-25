local test = import 'test.libsonnet';
local collectionId = test.value('collection_id');

test.mergeOptional(
  test.mergeOptional(
    {
      model: test.model(),
      name: 'test-evaluation-job-for-ragas-benchmark',
    } + if collectionId == '' then {
      benchmarks: [
        test.benchmark('ragas_rag_default', 'ragas', { num_examples: 1 }),
        test.benchmark('ragas_rag_full', 'ragas', { num_examples: 1 }),
      ],
      tags: ['benchmark-providers', 'ragas'],
    } else {},
    if collectionId != '' then test.collection() else null,
  ),
  test.experiment('my-test-experiment'),
)
