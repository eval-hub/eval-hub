local test = import 'test.libsonnet';

test.oobCollectionRefJobWithBenchmarks(
  'test-evaluation-job-oob-collection',
  'document-understanding-v1',
  [
    test.benchmark('common-words-extraction', 'ruler', { context_lengths: [4096], num_samples: 1 }),
    test.benchmark('frequency-words-extraction', 'ruler', { context_lengths: [4096], num_samples: 1 }),
    test.benchmark('qa-squad', 'ruler', { context_lengths: [4096], num_samples: 1 }),
    test.benchmark('qa-hotpotqa', 'ruler', { context_lengths: [4096], num_samples: 1 }),
    test.benchmark('niah-single-essay', 'ruler', { context_lengths: [4096], num_samples: 1 }),
    test.benchmark('variable-tracking', 'ruler', { context_lengths: [4096], num_samples: 1 }),
  ],
)
