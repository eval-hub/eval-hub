local test = import 'test.libsonnet';
local collectionId = test.value('collection_id');

test.mergeOptional(
  test.mergeOptional(
    {
      model: test.model(),
      name: 'test-evaluation-job-for-lm_evaluation_harness-benchmark',
    } + if collectionId == '' then {
      benchmarks: [
        test.benchmark('global_mmlu_full_am_anatomy', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('global_mmlu_full_am_clinical_knowledge', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('global_mmlu_full_am_medical_genetics', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('global_mmlu_full_am_professional_medicine', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('global_mmlu_full_ar_anatomy', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('global_mmlu_full_ar_clinical_knowledge', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('global_mmlu_full_ar_medical_genetics', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('global_mmlu_full_ar_professional_medicine', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('global_mmlu_full_bn_anatomy', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('lambada_openai', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('lambada_openai_mt_en', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('lambada_openai_mt_it', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('lambada_openai_mt_stablelm_es', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('lambada_openai_mt_stablelm_nl', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('lambada_standard_cloze_yaml', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('paloma_wikitext_103', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('wikitext', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('AraDiCE_truthfulqa_mc1_lev', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('metabench_truthfulqa_permute', 'lm_evaluation_harness', { num_examples: 1 }),
      ],
      tags: ['benchmark-providers', 'lm_evaluation_harness'],
    } else {},
    if collectionId != '' then test.collection() else null,
  ),
  test.experiment('my-test-experiment-lm_evaluation_harness'),
)
