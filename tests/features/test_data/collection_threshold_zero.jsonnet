local test = import 'test.libsonnet';
local harness = std.parseJson(std.extVar('harness'));

local thresholdZeroBenchmark() =
  {
    id: 'arc_easy',
    provider_id: 'lm_evaluation_harness',
    primary_score: {
      metric: 'acc_norm',
      lower_is_better: false,
    },
    pass_criteria: {
      threshold: 0.5,
    },
    parameters: {
      limit: 10,
      num_fewshot: 0,
      tokenizer: test.defaultTokenizer(),
    },
  } + if harness.disconnected then {
    test_data_ref: test.testDataRef(),
  } else {};

{
  name: 'test-benchmarks-collection-threshold-zero',
  category: 'test',
  description: 'Collection of benchmarks for FVT',
  pass_criteria: {
    threshold: 0,
  },
  benchmarks: [
    thresholdZeroBenchmark(),
    thresholdZeroBenchmark(),
  ],
}
