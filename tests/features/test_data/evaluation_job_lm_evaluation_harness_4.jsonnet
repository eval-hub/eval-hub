local test = import 'test.libsonnet';
local collectionId = test.value('collection_id');

test.mergeOptional(
  test.mergeOptional(
    {
      model: test.model(),
      name: 'test-evaluation-job-for-lm_evaluation_harness-benchmark',
    } + if collectionId == '' then {
      benchmarks: [
        test.benchmark('AraDiCE_ArabicMMLU_middle_social-science_civics_egy', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('AraDiCE_ArabicMMLU_middle_social-science_economics_lev', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('AraDiCE_ArabicMMLU_middle_social-science_social-science_egy', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('AraDiCE_ArabicMMLU_middle_stem_computer-science_lev', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('AraDiCE_ArabicMMLU_primary_social-science_geography_egy', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('AraDiCE_ArabicMMLU_primary_social-science_social-science_lev', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('AraDiCE_ArabicMMLU_primary_stem_natural-science_lev', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('AraDiCE_ArabicMMLU_univ_social-science_accounting_lev', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('AraDiCE_ArabicMMLU_univ_social-science_political-science_egy', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('AraDiCE_ArabicMMLU_univ_stem_computer-science_lev', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('arabic_leaderboard_arabic_mmlu_college_biology_light', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('agieval_logiqa_zh', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('bbh', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('bbh_cot_fewshot', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('bbh_cot_fewshot_causal_judgement', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('bbh_cot_fewshot_dyck_languages', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('bbh_cot_fewshot_hyperbaton', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('bbh_cot_fewshot_logical_deduction_three_objects', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('bbh_cot_fewshot_navigate', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('bbh_cot_fewshot_reasoning_about_colored_objects', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('bbh_cot_fewshot_snarks', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('bbh_cot_fewshot_tracking_shuffled_objects_five_objects', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('bbh_cot_fewshot_web_of_lies', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('bbh_cot_zeroshot', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('bbh_cot_zeroshot_causal_judgement', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('bbh_cot_zeroshot_dyck_languages', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('arabic_leaderboard_arabic_mmlu_anatomy', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('arabic_leaderboard_arabic_mmlu_clinical_knowledge', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('arabic_leaderboard_arabic_mmlu_medical_genetics', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('arabic_leaderboard_arabic_mmlu_professional_medicine', 'lm_evaluation_harness', { num_examples: 1 }),
      ],
      tags: ['benchmark-providers', 'lm_evaluation_harness'],
    } else {},
    if collectionId != '' then test.collection() else null,
  ),
  test.experiment('my-test-experiment-lm_evaluation_harness'),
)
