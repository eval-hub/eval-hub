local test = import 'test.libsonnet';

test.evalCardArcEasyJob(
  'evalcard_no_pass_criteria_test',
  'Test EvalCard generation for job without pass_criteria',
  ['evalcard', 'edge-case'],
  'evalcard_no_criteria_test',
  2,
)
