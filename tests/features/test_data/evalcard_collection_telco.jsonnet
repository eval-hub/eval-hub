local test = import 'test.libsonnet';

test.oobCollectionRefJobWithBenchmarks(
  'evalcard_collection_telco_test',
  'open-telco-v1',
  [
    test.benchmark('inspect/telemath', 'inspect', { num_examples: 5 }),
    test.benchmark('inspect/teleqna', 'inspect', { num_examples: 5 }),
    test.benchmark('inspect/telelogs', 'inspect', { num_examples: 5 }),
    test.benchmark('inspect/3gpp-tsg', 'inspect', { num_examples: 5 }),
  ],
) + {
  description: 'Job for card testing with telco collection',
  tags: ['evalcard', 'collection', 'telco'],
  experiment: {
    name: 'evalcard_collection_telco_experiment',
  },
  pass_criteria: {
    threshold: 0.5,
  },
}
