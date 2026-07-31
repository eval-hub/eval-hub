local test = import 'test.libsonnet';

test.evalCardArcEasyJob(
  'evalcard_shared_exp_job1',
  'First job in shared experiment',
  ['evalcard', 'shared'],
  'evalcard_shared_experiment',
)
