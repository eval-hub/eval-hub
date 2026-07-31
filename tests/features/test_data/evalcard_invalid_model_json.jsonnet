local test = import 'test.libsonnet';

test.evalCardArcEasyJob(
  'evalcard_json_validation_test',
  'Test that EvalCard artifact is valid JSON even for failed jobs',
  ['evalcard', 'validation'],
  'evalcard_json_test',
  2,
) + {
  model: {
    url: 'http://invalid.test/v1',
    name: 'invalid-model',
  },
}
