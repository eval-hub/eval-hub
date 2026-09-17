local test = import 'test.libsonnet';
local collectionId = test.value('collection_id');

test.mergeOptional(
  test.mergeOptional(
    {
      model: test.model(),
      name: 'test-evaluation-job-for-lm_evaluation_harness-benchmark',
    } + if collectionId == '' then {
      benchmarks: [
       test.benchmark('arc_easy', 'lm_evaluation_harness', { num_examples: 1 }),
       test.benchmark('AraDiCE_boolq_lev', 'lm_evaluation_harness', { num_examples: 1 }),
       test.benchmark('blimp_anaphor_gender_agreement', 'lm_evaluation_harness', { num_examples: 1 }),
       test.benchmark('blimp_animate_subject_trans', 'lm_evaluation_harness', { num_examples: 1 }),
       test.benchmark('blimp_coordinate_structure_constraint_complex_left_branch', 'lm_evaluation_harness', { num_examples: 1 }),
       test.benchmark('blimp_determiner_noun_agreement_2', 'lm_evaluation_harness', { num_examples: 1 }),
       test.benchmark('blimp_determiner_noun_agreement_with_adj_2', 'lm_evaluation_harness', { num_examples: 1 }),
       test.benchmark('blimp_determiner_noun_agreement_with_adjective_1', 'lm_evaluation_harness', { num_examples: 1 }),
       test.benchmark('blimp_existential_there_object_raising', 'lm_evaluation_harness', { num_examples: 1 }),
       test.benchmark('blimp_existential_there_subject_raising', 'lm_evaluation_harness', { num_examples: 1 }),
       test.benchmark('blimp_intransitive', 'lm_evaluation_harness', { num_examples: 1 }),
       test.benchmark('blimp_irregular_plural_subject_verb_agreement_1', 'lm_evaluation_harness', { num_examples: 1 }),
       test.benchmark('blimp_left_branch_island_simple_question', 'lm_evaluation_harness', { num_examples: 1 }),
       test.benchmark('blimp_npi_present_2', 'lm_evaluation_harness', { num_examples: 1 }),
       test.benchmark('blimp_passive_1', 'lm_evaluation_harness', { num_examples: 1 }),
       test.benchmark('AraDiCE_ArabicMMLU_egy', 'lm_evaluation_harness', { num_examples: 1 }),
       test.benchmark('AraDiCE_ArabicMMLU_high_humanities_history_lev', 'lm_evaluation_harness', { num_examples: 1 }),
       test.benchmark('AraDiCE_ArabicMMLU_high_humanities_philosophy_egy', 'lm_evaluation_harness', { num_examples: 1 }),
       test.benchmark('AraDiCE_ArabicMMLU_high_language_arabic-language_lev', 'lm_evaluation_harness', { num_examples: 1 }),
       test.benchmark('AraDiCE_ArabicMMLU_lev', 'lm_evaluation_harness', { num_examples: 1 }),
       test.benchmark('AraDiCE_ArabicMMLU_middle_humanities_islamic-studies_egy', 'lm_evaluation_harness', { num_examples: 1 }),
       test.benchmark('AraDiCE_ArabicMMLU_middle_language_arabic-language_lev', 'lm_evaluation_harness', { num_examples: 1 }),
       test.benchmark('AraDiCE_ArabicMMLU_na_humanities_islamic-studies_egy', 'lm_evaluation_harness', { num_examples: 1 }),
       test.benchmark('AraDiCE_ArabicMMLU_na_language_arabic-language-general_lev', 'lm_evaluation_harness', { num_examples: 1 }),
       test.benchmark('AraDiCE_ArabicMMLU_na_other_driving-test_egy', 'lm_evaluation_harness', { num_examples: 1 }),
      ],
      tags: ['benchmark-providers', 'lm_evaluation_harness'],
    } else {},
    if collectionId != '' then test.collection() else null,
  ),
  test.experiment('my-test-experiment-lm_evaluation_harness'),
)
