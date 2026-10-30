local test = import 'test.libsonnet';
local collectionId = test.value('collection_id');
// Keep the retained profiles suitable for smoke tests. The prompt cap is soft,
// so also bound each Garak scan with an explicit timeout.
local scanParameters = {
  timeout_seconds: 600,
  garak_config: {
    run: { generations: 1, soft_probe_prompt_cap: 1 },
    plugins: { extended_detectors: false },
  },
};
// atkgen.Tox has its own conversation loop, independent of generations and the
// soft prompt cap. Keep it to one conversation and one turn for this smoke test.
local ethicsParameters = scanParameters {
  garak_config+: {
    plugins+: {
      probes: {
        atkgen: {
          Tox: { convs_per_generation: 1, max_calls_per_conv: 1 },
        },
      },
    },
  },
};

test.mergeOptional(
  test.mergeOptional(
    {
      model: test.model(),
      name: 'test-evaluation-job-for-garak-benchmark',
    } + if collectionId == '' then {
      benchmarks: [
        test.benchmark('quick', 'garak', scanParameters),
        test.benchmark('avid_ethics', 'garak', ethicsParameters),
        test.benchmark('avid_performance', 'garak', scanParameters),
      ],
      tags: ['benchmark-providers', 'garak'],
    } else {},
    if collectionId != '' then test.collection() else null,
  ),
  test.experiment('my-test-experiment-garak-benchmark'),
)
