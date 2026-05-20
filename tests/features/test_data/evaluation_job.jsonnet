local test = import 'test.libsonnet';
local collectionId = test.value('collection_id');

test.mergeOptional(
  test.mergeOptional(
    {
      model: test.model(),
      name: 'test-evaluation-job',
    } + if collectionId == '' then {
      benchmarks: [
        {
          id: 'arc_easy',
          provider_id: 'lm_evaluation_harness',
          parameters: {
            num_examples: 10,
            num_fewshot: 3,
            limit: 5,
            tokenizer: 'google/flan-t5-small',
          },
        },
      ],
      tags: ['environment'],
    } else {},
    if collectionId != '' then test.collection() else null,
  ),
  test.experiment('my-test-experiment'),
)
