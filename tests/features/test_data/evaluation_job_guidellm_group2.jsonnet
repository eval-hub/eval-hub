local test = import 'test.libsonnet';
local collectionId = test.value('collection_id');

test.mergeOptional(
  test.mergeOptional(
    {
      model: test.model(),
      name: 'test-evaluation-job-for-guidellm-benchmark',
    } + if collectionId == '' then {
      benchmarks: [
        test.benchmark('poisson', 'guidellm', { num_examples: 1 }),
        test.benchmark('quick_perf_test', 'guidellm', { num_examples: 1 }),
        test.benchmark('comprehensive_perf_test', 'guidellm', { num_examples: 1 }),
      ],
      tags: ['benchmark-providers', 'guidellm'],
    } else {},
    if collectionId != '' then test.collection() else null,
  ),
  test.experiment('my-test-experiment-guidellm'),
)
