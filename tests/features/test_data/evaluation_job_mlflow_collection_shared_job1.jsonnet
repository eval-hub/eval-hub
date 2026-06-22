local test = import 'test.libsonnet';

test.mergeOptional(
  test.oobCollectionRefJobFromValue('automation_shared_experiment_with_collections_job_1'),
  {
    experiment: {
      name: 'automation_shared_experiment',
    },
  },
)
