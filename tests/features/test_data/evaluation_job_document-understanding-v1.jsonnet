local test = import 'test.libsonnet';
local rulerBenchmark(id) = {
  id: id,
  provider_id: 'ruler',
  parameters: {
    context_lengths: [4096],
    num_samples: 1,
  },
} + test.hardwareConfigQueue();

test.oobCollectionRefJobWithBenchmarks(
  'test-evaluation-job-oob-collection',
  'document-understanding-v1',
  [
    rulerBenchmark('common-words-extraction'),
    rulerBenchmark('frequency-words-extraction'),
    rulerBenchmark('qa-squad'),
    rulerBenchmark('qa-hotpotqa'),
    rulerBenchmark('niah-single-essay'),
    rulerBenchmark('variable-tracking'),
  ],
)
