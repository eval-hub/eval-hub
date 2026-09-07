local test = import 'test.libsonnet';
local collectionId = test.value('collection_id');

test.mergeOptional(
  test.mergeOptional(
    {
      model: test.model(),
      name: 'test-evaluation-job-for-lm_evaluation_harness-benchmark',
    } + if collectionId == '' then {
      benchmarks: [
       test.benchmark('blimp', 'lm_evaluation_harness', { num_examples: 1 }),
       test.benchmark('AraDiCE_ArabicMMLU_na_other_general-knowledge_lev', 'lm_evaluation_harness', { num_examples: 1 }),
       test.benchmark('AraDiCE_ArabicMMLU_primary_humanities_islamic-studies_egy', 'lm_evaluation_harness', { num_examples: 1 }),
       test.benchmark('AraDiCE_ArabicMMLU_primary_language_arabic-language_lev', 'lm_evaluation_harness', { num_examples: 1 }),
       test.benchmark('AraDiCE_ArabicMMLU_univ_other_management_egy', 'lm_evaluation_harness', { num_examples: 1 }),
      ],
      tags: ['benchmark-providers', 'lm_evaluation_harness'],
    } else {},
    if collectionId != '' then test.collection() else null,
  ),
  test.experiment('my-test-experiment-lm_evaluation_harness'),
)
