local test = import 'test.libsonnet';

test.oobCollectionRefJobWithBenchmarks(
  'test-evaluation-job-oob-collection',
  'multimodal-v1',
  [
    local benchmark = test.benchmark('inspect/docvqa', 'inspect', { num_examples: 1 });
    benchmark + {
      hardware_config:
        (if std.objectHas(benchmark, 'hardware_config') then benchmark.hardware_config else {}) + {
          memory: {
            request: '8Gi',
            limit: '8Gi',
          },
        },
    }
  ],
)
