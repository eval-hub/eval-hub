local test = import 'test.libsonnet';

// Shared EvalCard arc_easy + MLflow experiment payload (disconnected-aware via test.benchmark).
test.evalCardArcEasyJob(
  'evalcard_arc_easy_test',
  'Job for EvalCard testing',
  ['evalcard'],
  'evalcard_arc_easy_experiment',
)
