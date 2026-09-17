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
      test.benchmark('owasp_llm_top10', 'garak', { num_examples: 1 }),
      test.benchmark('avid', 'garak', { num_examples: 1 }),
      test.benchmark('avid_security', 'garak', { num_examples: 1 }),
      test.benchmark('avid_ethics', 'garak', { num_examples: 1 }),
      test.benchmark('avid_performance', 'garak', { num_examples: 1 }),
      test.benchmark('quality', 'garak', { num_examples: 1 }),
      test.benchmark('cwe', 'garak', { num_examples: 1 }),
      test.benchmark('intents', 'garak', {
        num_examples: 1,
        intents_models: {
          judge: {
            url: test.env('MODEL_URL', 'http://test.com'),
            name: test.env('MODEL_NAME', 'test'),
          },
        },
        sdg_model: test.env('MODEL_NAME', 'test'),
        sdg_api_base: test.env('MODEL_URL', 'http://test.com'),
      }),
      ],
      tags: ['benchmark-providers', 'garak'],
    } else {},
    if collectionId != '' then test.collection() else null,
  ),
  test.experiment('my-test-experiment-garak-benchmark'),
)
