local test = import 'test.libsonnet';

{
  name: 'test-evaluation-job-queue-collection',
  collection: {
    id: 'open-telco-v1',
    benchmarks: [
      test.benchmark('inspect/telemath', 'inspect', { num_examples: 5 }) + {
        hardware_config: {
          queue: test.queueConfig(),
        },
      },
      test.benchmark('inspect/teleqna', 'inspect', { num_examples: 5 }) + {
        hardware_config: {
          queue: test.queueConfig(),
        },
      },
      test.benchmark('inspect/telelogs', 'inspect', { num_examples: 5 }) + {
        hardware_config: {
          queue: test.queueConfig(),
        },
      },
      test.benchmark('inspect/3gpp-tsg', 'inspect', { num_examples: 5 }) + {
        hardware_config: {
          queue: test.queueConfig(),
        },
      },
    ],
  },
  model: test.model(),
}
