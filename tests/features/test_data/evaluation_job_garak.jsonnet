local test = import 'test.libsonnet';
local collectionId = test.value('collection_id');

test.mergeOptional(
  test.mergeOptional(
    {
      model: test.model(),
      name: 'test-evaluation-job-for-garak-benchmark',
    } + if collectionId == '' then {
      benchmarks: [
        test.benchmark('quick', 'garak', { num_examples: 1 }),
        test.benchmark('avid_ethics', 'garak', { num_examples: 1 }),
        test.benchmark('avid_performance', 'garak', { num_examples: 1 }),
      ],
      tags: ['benchmark-providers', 'garak'],
    } else {},
    if collectionId != '' then test.collection() else null,
  ),
  test.experiment('my-test-experiment-garak-benchmark'),
)
