local test = import 'test.libsonnet';

test.evalCardArcEasyJob(
  'evalcard_error_structure_test',
  'Test error_message structure in EvalCard for failed job',
  ['evalcard', 'error'],
  'evalcard_error_test',
  2,
) + {
  model: {
    url: 'http://invalid-model.test/v1',
    name: 'invalid-model',
  },
}
