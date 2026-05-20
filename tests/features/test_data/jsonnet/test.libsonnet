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
