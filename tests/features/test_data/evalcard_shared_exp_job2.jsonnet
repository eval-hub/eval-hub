local test = import 'test.libsonnet';

test.evalCardArcEasyJob(
  'evalcard_shared_exp_job2',
  'Second job in shared experiment',
  ['evalcard', 'shared'],
  'evalcard_shared_experiment',
)
