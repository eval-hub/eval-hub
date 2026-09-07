local test = import 'test.libsonnet';

{
  name: 'test-evaluation-job-queue-collection',
  collection: {
    id: 'open-telco-v1',
    benchmarks: [
      test.benchmark('telemath', 'inspect', { num_examples: 5 }) + {
        hardware_config: {
          queue: test.queueConfig(),
        },
      },
      test.benchmark('teleqna', 'inspect', { num_examples: 5 }) + {
        hardware_config: {
          queue: test.queueConfig(),
        },
      },
      test.benchmark('telelogs', 'inspect', { num_examples: 5 }) + {
        hardware_config: {
          queue: test.queueConfig(),
        },
      },
      test.benchmark('3gpp-tsg', 'inspect', { num_examples: 5 }) + {
        hardware_config: {
          queue: test.queueConfig(),
        },
      },
    ],
  },
  model: test.model(),
}
