local t = import 'jsonnet/test.libsonnet';

{
  name: 'test-evaluation-job-for-inspect-drop',
  model: t.model(),
  benchmarks: [
    t.benchmark('inspect/drop', 'inspect', {
      num_examples: 3,
    } + if t.disconnected() then {
      task_args: {
        fewshot: 1,
        fewshot_shuffle: false,
      },
    } else {}),
  ],
}
