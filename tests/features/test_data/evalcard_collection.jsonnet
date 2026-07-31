local test = import 'test.libsonnet';

test.evalCardToxicityCollectionJob(
  'evalcard_collection_test',
  'Job for card testing with collection',
  ['evalcard', 'collection'],
  'evalcard_collection_experiment',
) + {
  pass_criteria: {
    threshold: 0.5,
  },
}
