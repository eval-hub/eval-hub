local test = import 'test.libsonnet';
local collectionId = test.value('collection_id');

test.mergeOptional(
  test.mergeOptional(
    {
      model: test.model(),
      name: 'test-evaluation-job-for-guidellm-benchmark',
    } + if collectionId == '' then {
      benchmarks: [
        test.benchmark('throughput', 'guidellm', {
          num_examples: 10,
          max_requests: 10,
          max_seconds: 30,
          rate: 1,
        }),
      ],
      tags: ['benchmark-providers', 'guidellm'],
    } else {},
    if collectionId != '' then test.collection() else null,
  ),
  test.experiment('my-test-experiment-guidellm'),
)
