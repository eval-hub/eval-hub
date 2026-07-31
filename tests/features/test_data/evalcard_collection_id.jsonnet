local test = import 'test.libsonnet';

test.evalCardToxicityCollectionJob(
  'evalcard_collection_id_test',
  'Test card context has collection_id',
  ['evalcard', 'collection'],
  'evalcard_collection_id_experiment',
)
