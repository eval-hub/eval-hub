local test = import 'test.libsonnet';
local collectionId = test.value('collection_id');

test.mergeOptional(
  test.mergeOptional(
    {
      model: test.model(),
      name: 'test-evaluation-job-for-lighteval-benchmark-greedy_until-group2',
    } + if collectionId == '' then {
      benchmarks: [
        test.benchmark('math', 'lighteval', {
          num_examples: 1,
          parameters: { generation_parameters: { temperature: 0.1 } },
        }),
        test.benchmark('math:algebra', 'lighteval', {
          num_examples: 1,
          parameters: { generation_parameters: { temperature: 0.1 } },
        }),
        test.benchmark('math:counting_and_probability', 'lighteval', {
          num_examples: 1,
          parameters: { generation_parameters: { temperature: 0.1 } },
        }),
        test.benchmark('piqa', 'lighteval', { num_examples: 1 }),
        test.benchmark('physical_commonsense', 'lighteval', { num_examples: 1 }),
      ],
      tags: ['benchmark-providers', 'lighteval', 'greedy_until'],
    } else {},
    if collectionId != '' then test.collection() else null,
  ),
  test.experiment('my-test-experiment-lighteval-greedy_until-group2'),
)
