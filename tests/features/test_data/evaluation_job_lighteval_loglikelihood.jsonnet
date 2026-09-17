local test = import 'test.libsonnet';
local collectionId = test.value('collection_id');

test.mergeOptional(
  test.mergeOptional(
    {
      model: test.model(),
      name: 'test-evaluation-job-for-lighteval-benchmark-loglikelihood',
    } + if collectionId == '' then {
      benchmarks: [
        test.benchmark('commonsense_reasoning', 'lighteval', { num_examples: 1 }),
        test.benchmark('scientific_reasoning', 'lighteval', { num_examples: 1 }),
        test.benchmark('physical_commonsense', 'lighteval', { num_examples: 1 }),
        test.benchmark('truthfulness', 'lighteval', { num_examples: 1 }),
        test.benchmark('math', 'lighteval', { num_examples: 1 }),
        test.benchmark('language_understanding', 'lighteval', { num_examples: 1 }),
        test.benchmark('winogrande', 'lighteval', { num_examples: 1 }),
        test.benchmark('arc:easy', 'lighteval', { num_examples: 1 }),
        test.benchmark('arc:challenge', 'lighteval', { num_examples: 1 }),
        test.benchmark('piqa', 'lighteval', { num_examples: 1 }),
        test.benchmark('truthfulqa:mc', 'lighteval', { num_examples: 1 }),
        test.benchmark('math:algebra', 'lighteval', { num_examples: 1 }),
        test.benchmark('math:counting_and_probability', 'lighteval', { num_examples: 1 }),
        test.benchmark('glue:cola', 'lighteval', { num_examples: 1 }),
        test.benchmark('glue:sst2', 'lighteval', { num_examples: 1 }),
        test.benchmark('glue:mrpc', 'lighteval', { num_examples: 1 }),
        test.benchmark('gpqa:diamond', 'lighteval', { num_examples: 1 }),
      ],
      tags: ['benchmark-providers', 'lighteval','loglikelihood'],
    } else {},
    if collectionId != '' then test.collection() else null,
  ),
  test.experiment('my-test-experiment-lighteval-loglikelihood'),
)
