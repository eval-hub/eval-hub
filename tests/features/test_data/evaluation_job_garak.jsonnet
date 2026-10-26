local test = import 'test.libsonnet';
local collectionId = test.value('collection_id');
// Exercise each profile with a reduced workload. Garak's prompt cap is soft,
// so also enforce a scan timeout below the FVT job wait deadline.
local scanParameters = {
  timeout_seconds: 600,
  garak_config: {
    run: { generations: 1, soft_probe_prompt_cap: 1 },
    plugins: { extended_detectors: false },
  },
};
local modelName = test.env('MODEL_NAME', 'test');

test.mergeOptional(
  test.mergeOptional(
    {
      model: test.model(),
      name: 'test-evaluation-job-for-garak-benchmark',
    } + if collectionId == '' then {
      benchmarks: [
      test.benchmark('quick', 'garak', scanParameters),
      test.benchmark('owasp_llm_top10', 'garak', scanParameters),
      test.benchmark('avid', 'garak', scanParameters),
      test.benchmark('avid_security', 'garak', scanParameters),
      test.benchmark('avid_ethics', 'garak', scanParameters),
      test.benchmark('avid_performance', 'garak', scanParameters),
      test.benchmark('quality', 'garak', scanParameters),
      test.benchmark('cwe', 'garak', scanParameters),
      test.benchmark('intents', 'garak', scanParameters + {
        // Keep prompt generation and the intent probe suitable for a smoke test.
        probes: 'spo.SPOIntent',
        sdg_num_samples: 1,
        sdg_max_tokens: 512,
        probe_options: { spo: { SPOIntent: { max_dan_samples: 1 } } },
        intents_models: {
          judge: {
            url: test.env('MODEL_URL', 'http://test.com'),
            name: test.env('MODEL_NAME', 'test'),
          },
        },
        sdg_model: if std.startsWith(modelName, 'openai/') then modelName else 'openai/' + modelName,
        sdg_api_base: test.env('MODEL_URL', 'http://test.com'),
      }),
      ],
      tags: ['benchmark-providers', 'garak'],
    } else {},
    if collectionId != '' then test.collection() else null,
  ),
  test.experiment('my-test-experiment-garak-benchmark'),
)
