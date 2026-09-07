local test = import 'test.libsonnet';
local collectionId = test.value('collection_id');

test.mergeOptional(
  test.mergeOptional(
    {
      model: test.model(),
      name: 'test-evaluation-job-for-lighteval-benchmark-greedy_until',
    } + if collectionId == '' then {
      benchmarks: [
        test.benchmark('knowledge', 'lighteval', { num_examples: 1 }),
        test.benchmark('hellaswag', 'lighteval', { num_examples: 1 }),
        test.benchmark('openbookqa', 'lighteval', { num_examples: 1 }),
        test.benchmark('gsm8k', 'lighteval', { num_examples: 1 }),
        test.benchmark('aime24', 'lighteval', { num_examples: 1 }),
        test.benchmark('aime25', 'lighteval', { num_examples: 1 }),
        test.benchmark('mmlu', 'lighteval', { num_examples: 1 }),
        test.benchmark('triviaqa', 'lighteval', { num_examples: 1 }),
        test.benchmark('math_500', 'lighteval', { num_examples: 1 }),
        test.benchmark('lcb:codegeneration_v6', 'lighteval', { num_examples: 1 }),
        test.benchmark('truthfulqa:gen', 'lighteval', { num_examples: 1 }),

      ],
      tags: ['benchmark-providers', 'lighteval','greedy_until'],
    } else {},
    if collectionId != '' then test.collection() else null,
  ),
  test.experiment('my-test-experiment-lighteval-greedy_until'),
)
