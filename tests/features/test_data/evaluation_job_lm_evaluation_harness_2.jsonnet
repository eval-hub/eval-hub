local test = import 'test.libsonnet';
local collectionId = test.value('collection_id');

test.mergeOptional(
  test.mergeOptional(
    {
      model: test.model(),
      name: 'test-evaluation-job-for-lm_evaluation_harness-benchmark',
    } + if collectionId == '' then {
      benchmarks: [
       test.benchmark('AraDiCE_openbookqa_eng', 'lm_evaluation_harness', { num_examples: 1 }),
       test.benchmark('arabic_leaderboard_arabic_mt_boolq', 'lm_evaluation_harness', { num_examples: 1 }),
       test.benchmark('arabic_leaderboard_arabic_mt_boolq_light', 'lm_evaluation_harness', { num_examples: 1 }),
       test.benchmark('arabic_mt_boolq_light', 'lm_evaluation_harness', { num_examples: 1 }),
       test.benchmark('leaderboard_bbh_salient_translation_error_detection', 'lm_evaluation_harness', { num_examples: 1 }),
       test.benchmark('aclue_ancient_chinese_culture', 'lm_evaluation_harness', { num_examples: 1, trust_remote_code: true }),
       test.benchmark('african_flores', 'lm_evaluation_harness', { num_examples: 1 }),
       test.benchmark('afrixnli-irokobench', 'lm_evaluation_harness', { num_examples: 1 }),
       test.benchmark('afrixnli_amh_prompt_2', 'lm_evaluation_harness', { num_examples: 1 }),
       test.benchmark('afrixnli_amh_prompt_5', 'lm_evaluation_harness', { num_examples: 1 }),
       test.benchmark('afrixnli_en_direct_ewe', 'lm_evaluation_harness', { num_examples: 1 }),
       test.benchmark('afrixnli_en_direct_ibo', 'lm_evaluation_harness', { num_examples: 1 }),
       test.benchmark('afrixnli_en_direct_lug', 'lm_evaluation_harness', { num_examples: 1 }),
       test.benchmark('afrixnli_en_direct_sot', 'lm_evaluation_harness', { num_examples: 1 }),
       test.benchmark('afrixnli_en_direct_wol', 'lm_evaluation_harness', { num_examples: 1 }),
       test.benchmark('afrixnli_en_direct_zul', 'lm_evaluation_harness', { num_examples: 1 }),
       test.benchmark('AraDiCE_ArabicMMLU_primary_stem_math_egy', 'lm_evaluation_harness', { num_examples: 1 }),
       test.benchmark('arabic_leaderboard_arabic_mmlu_college_mathematics_light', 'lm_evaluation_harness', { num_examples: 1 }),
       test.benchmark('arabic_leaderboard_arabic_mmlu_high_school_mathematics', 'lm_evaluation_harness', { num_examples: 1 }),
       test.benchmark('cmmlu_college_mathematics', 'lm_evaluation_harness', { num_examples: 1, trust_remote_code: true }),
       test.benchmark('cmmlu_high_school_mathematics', 'lm_evaluation_harness', { num_examples: 1, trust_remote_code: true }),
       test.benchmark('global_mmlu_full_am_high_school_mathematics', 'lm_evaluation_harness', { num_examples: 1 }),
       test.benchmark('global_mmlu_full_ar_high_school_mathematics', 'lm_evaluation_harness', { num_examples: 1 }),
       test.benchmark('global_mmlu_full_bn_high_school_mathematics', 'lm_evaluation_harness', { num_examples: 1 }),
       test.benchmark('global_mmlu_full_cs_high_school_mathematics', 'lm_evaluation_harness', { num_examples: 1 }),
       test.benchmark('global_mmlu_full_de_high_school_mathematics', 'lm_evaluation_harness', { num_examples: 1 }),
       test.benchmark('global_mmlu_full_el_high_school_mathematics', 'lm_evaluation_harness', { num_examples: 1 }),
       test.benchmark('global_mmlu_full_en_high_school_mathematics', 'lm_evaluation_harness', { num_examples: 1 }),
       test.benchmark('global_mmlu_full_es_high_school_mathematics', 'lm_evaluation_harness', { num_examples: 1 }),
       test.benchmark('global_mmlu_full_fa_high_school_mathematics', 'lm_evaluation_harness', { num_examples: 1 }),
      ],
      tags: ['benchmark-providers', 'lm_evaluation_harness'],
    } else {},
    if collectionId != '' then test.collection() else null,
  ),
  test.experiment('my-test-experiment-lm_evaluation_harness'),
)
