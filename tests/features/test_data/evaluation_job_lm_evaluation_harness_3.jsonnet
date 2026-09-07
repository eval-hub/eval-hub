local test = import 'test.libsonnet';
local collectionId = test.value('collection_id');

test.mergeOptional(
  test.mergeOptional(
    {
      model: test.model(),
      name: 'test-evaluation-job-for-lm_evaluation_harness-benchmark',
    } + if collectionId == '' then {
      benchmarks: [
        test.benchmark('global_mmlu_full_fil_high_school_mathematics', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('AraDiCE_piqa_lev', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('AraDiCE_winogrande_eng', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('arabic_leaderboard_arabic_mt_copa', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('arabic_leaderboard_arabic_mt_copa_light', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('arabic_leaderboard_arabic_mt_hellaswag', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('arabic_leaderboard_arabic_mt_hellaswag_light', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('arabic_leaderboard_arabic_mt_piqa', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('arabic_leaderboard_arabic_mt_piqa_light', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('arabic_mt_hellaswag', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('arabic_mt_piqa', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('copa_ar', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('copal_id_colloquial', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('darijahellaswag', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('egyhellaswag', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('hellaswag_ar', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('arabic_leaderboard_arabic_mt_race', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('arabic_leaderboard_arabic_mt_race_light', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('arabic_mt_race_light', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('blimp_drop_argument', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('bigbench_gre_reading_comprehension_multiple_choice', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('eus_reading', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('longbench_qasper', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('qasper_freeform', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('ruler_qa_squad', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('scrolls_qasper', 'lm_evaluation_harness', { num_examples: 1, trust_remote_code: true }),
        test.benchmark('AraDiCE_ArabicMMLU_high_social-science_economics_egy', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('AraDiCE_ArabicMMLU_high_social-science_geography_lev', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('AraDiCE_ArabicMMLU_high_stem_computer-science_egy', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('AraDiCE_ArabicMMLU_high_stem_physics_lev', 'lm_evaluation_harness', { num_examples: 1 }),
      ],
      tags: ['benchmark-providers', 'lm_evaluation_harness'],
    } else {},
    if collectionId != '' then test.collection() else null,
  ),
  test.experiment('my-test-experiment-lm_evaluation_harness'),
)
