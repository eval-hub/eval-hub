local test = import 'test.libsonnet';
local collectionId = test.value('collection_id');

test.mergeOptional(
  test.mergeOptional(
    {
      model: test.model(),
      name: 'test-evaluation-job-for-lm_evaluation_harness-benchmark',
    } + if collectionId == '' then {
      benchmarks: [
        test.benchmark('tinyTruthfulQA', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('bigbench_code_line_description_multiple_choice', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('ceval-valid_college_programming', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('code2text_javascript', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('code2text_ruby', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('humaneval', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('humaneval_instruct', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('mbpp', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('leaderboard_ifeval', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('leaderboard_bbh', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('leaderboard_gpqa', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('leaderboard_mmlu_pro', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('leaderboard_musr', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('leaderboard_math_hard', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('bbq', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('crows_pairs_english', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('ethics_cm', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('toxigen', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('truthfulqa_mc1', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('bigbench_hhh_alignment_multiple_choice', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('winogender', 'lm_evaluation_harness', { num_examples: 1 }),
        test.benchmark('ifeval', 'lm_evaluation_harness', { num_examples: 1 }),
      ],
      tags: ['benchmark-providers', 'lm_evaluation_harness'],
    } else {},
    if collectionId != '' then test.collection() else null,
  ),
  test.experiment('my-test-experiment-lm_evaluation_harness'),
)
