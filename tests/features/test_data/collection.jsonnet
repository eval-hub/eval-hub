local test = import 'test.libsonnet';

{
  name: 'test-benchmarks-collection',
  description: 'Collection of benchmarks for FVT',
  category: 'test',
  benchmarks: [
    test.arcEasyBenchmark({ weight: 3 }),
  ],
}
