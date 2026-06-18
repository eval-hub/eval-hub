// Shared helpers for FVT test payloads. Reads scenario state from std.extVar('harness'),
// populated by the Go test harness (process env plus jsonnetHarnessEnv / jsonnetHarnessEnvOmit
// on scenarioConfig, saved values, MLflow flag).
local harness = std.parseJson(std.extVar('harness'));

{
  // Resolves an environment variable, or fallback when unset.
  env(name, fallback='')::
    if std.objectHas(harness.env, name) then harness.env[name] else fallback,

  // Resolves a value saved in the scenario via "saved as value:<name>".
  value(name, default='')::
    if std.objectHas(harness, 'values') && std.objectHas(harness.values, name) then harness.values[name] else default,

  // Collection reference from a saved value (e.g. collection_id); null when unset (use with mergeOptional).
  collection(idKey='collection_id')::
    local id = $.value(idKey);
    if id != '' then {
      collection: {
        id: id,
      },
    },

  // Experiment name when MLflow is configured; empty otherwise (matches {{mlflow:...}}).
  mlflow(name)::
    if harness.mlflow_enabled then name else '',

  // Tokenizer path for connected vs disconnected cluster FVT.
  defaultTokenizer()::
    if harness.disconnected then '/test_data/tokenizer' else 'google/flan-t5-small',

  // Offline test data reference for disconnected runs.
  testDataRef()::
    {
      s3: {
        bucket: $.env('TEST_DATA_S3_BUCKET', 'mlpipeline'),
        key: $.env('TEST_DATA_S3_KEY', 'offline'),
        secret_ref: $.env('TEST_DATA_S3_SECRET_REF', 'minio-test'),
      },
    },

  // Evaluation/collection benchmark with disconnected-aware tokenizer and optional test_data_ref.
  benchmark(id, providerId, parameters)::
    local base = {
      id: id,
      provider_id: providerId,
      parameters: {
        tokenizer: $.defaultTokenizer(),
      } + parameters,
    };
    if harness.disconnected then base + { test_data_ref: $.testDataRef() } else base,

  // arc_easy benchmark with common FVT defaults; extra parameters override or extend.
  arcEasyBenchmark(parameters={})::
    $.benchmark('arc_easy', 'lm_evaluation_harness', {
      num_examples: 10,
      num_fewshot: 3,
      limit: 5,
    } + parameters),

  // Default benchmark for evaluation_job.jsonnet (disconnected vs connected FVT).
  defaultBenchmark():: $.arcEasyBenchmark({}),

  // OOB collection job with per-benchmark overrides (disconnected-aware tokenizer + test_data_ref).
  oobCollectionJob(collectionId, benchmarks)::
    {
      name: 'test-evaluation-job-oob-collection',
      collection: {
        id: collectionId,
        benchmarks: benchmarks,
      },
      model: $.model(),
    },

  // Default per-benchmark example cap for OOB collection FVT.
  defaultOobNumExamples():: 5,

  safetyAndFairnessV1BenchmarkIds()::
    ['truthfulqa_mc1', 'toxigen', 'winogender', 'crows_pairs_english', 'bbq', 'ethics_cm'],

  toxicityAndEthicalPrinciplesBenchmarkIds()::
    ['toxigen', 'truthfulqa_mc1', 'bigbench_hhh_alignment_multiple_choice'],

  leaderboardV2BenchmarkIds()::
    ['leaderboard_ifeval', 'leaderboard_bbh', 'leaderboard_gpqa', 'leaderboard_mmlu_pro', 'leaderboard_musr', 'leaderboard_math_hard'],

  // OOB collection with per-benchmark parameter overrides for faster cluster FVT.
  oobCollectionRefJobWithBenchmarks(name, collectionId, benchmarks)::
    {
      name: name,
      model: $.model(),
      collection: {
        id: collectionId,
        benchmarks: benchmarks,
      },
    },

  // num_examples caps runtime for OOB collection FVT without conflicting with collection limit.
  oobCollectionParameterOverrides(numExamples)::
    {
      num_examples: numExamples,
    },

  // Applies defaultOobNumExamples() to each benchmark id in an OOB collection.
  oobCollectionRefJobWithLimit(name, collectionId, benchmarkIds, numExamples=null)::
    local n = if numExamples == null then $.defaultOobNumExamples() else numExamples;
    $.oobCollectionRefJobWithBenchmarks(
      name,
      collectionId,
      std.map(function(id) $.benchmark(id, 'lm_evaluation_harness', $.oobCollectionParameterOverrides(n)), benchmarkIds),
    ),

  // OOB collection by id only (server expands to full collection). model.auth is included only when
  // MODEL_AUTH_SECRET_REF is set in the harness env (see model()). Prefer oobCollectionRefJobWithLimit for FVT.
  oobCollectionRefJob(name, collectionId)::
    {
      name: name,
      model: $.model(),
      collection: {
        id: collectionId,
      },
    },

  // Same as oobCollectionRefJob with collection id from a saved scenario value.
  oobCollectionRefJobFromValue(name, collectionIdKey='collection_id')::
    $.oobCollectionRefJob(name, $.value(collectionIdKey)),

  // Default evaluation job model block used across many scenarios.
  model()::
    local secretRef = $.env('MODEL_AUTH_SECRET_REF', '');
    {
      url: $.env('MODEL_URL', 'http://test.com'),
      name: $.env('MODEL_NAME', 'test'),
    } + if secretRef != '' then {
      auth: {
        secret_ref: secretRef,
      },
    } else {},

  // Merge base with an optional object; optional may be null (adds nothing).
  mergeOptional(base, optional)::
    if optional == null then base else base + optional,

  // MLflow experiment block, or null when MLflow is not configured (use with mergeOptional).
  experiment(name, tags=[{ key: 'environment', value: 'test' }])::
    local experimentName = $.mlflow(name);
    if experimentName != '' then {
      experiment: {
        name: experimentName,
        tags: tags,
      },
    },
}
