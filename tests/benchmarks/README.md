# Benchmark smoke tests

This suite discovers providers through EvalHub, reads each provider's benchmark
list, and submits one independent evaluation job per configured benchmark.
It complements the grouped provider scenarios in `tests/features`. These are
ordinary Go integration tests, not Go performance benchmarks or accuracy checks.

## Run

Use an already running EvalHub service with real provider runtimes and a reachable
model endpoint. The local FVT mock runtime does not validate real adapters.

```bash
export SERVER_URL=https://evalhub.example
export EVALHUB_TOKEN=...          # if the service requires a bearer token
export X_TENANT=my-namespace     # required in cluster mode
export X_USER=my-user           # required when calling the service directly in cluster mode
export MODEL_NAME=org/model
export MODEL_URL=https://model.example/v1
export MODEL_AUTH_SECRET_REF=model-credentials  # if the model requires authentication
export EVALHUB_CA_CERT_PATH=/path/to/ca.pem      # optional private CA
make test-benchmarks
```

The service's authentication proxy determines the forwarded identity when used.
Model credentials belong in the referenced Kubernetes Secret, not in JSON fixtures.
TLS verification remains enabled. Identity headers are not forwarded to redirects.

Default concurrency is **2 evaluation jobs across all providers**. Each job uses
its fixture deadline (initially five minutes), and the suite has a 30-minute
deadline. All 341 benchmarks can take longer than 30 minutes even with tiny inputs:
select a provider or benchmark first, or increase the suite deadline deliberately.

```bash
make test-benchmarks BENCHMARK_RUN='TestBenchmarkSmoke/lm_evaluation_harness/arc_easy$'
make test-benchmarks BENCHMARK_RUN='TestBenchmarkSmoke/garak/' BENCHMARK_PARALLEL=2
make test-benchmarks BENCHMARK_TIMEOUT=2h
```

Equivalent direct invocation:

```bash
BENCHMARK_SMOKE=1 go test ./tests/benchmarks \
  -run TestBenchmarkSmoke -count=1 -parallel=2 -timeout=30m -v
```

`BENCHMARK_CONFIG_DIR` overrides the fixture directory (relative to
`tests/benchmarks`, where Go runs the tests). `BENCHMARK_POLL_INTERVAL` overrides
the default `2s` polling interval. Standard `go test -json` output provides
per-benchmark results for CI consumers.

Ordinary `go test ./tests/benchmarks/...` runs offline harness checks and skips live
evaluations. Live runs fail on invalid configuration, discovery errors, failed
jobs, timeouts, or zero submitted benchmarks. Missing fixture files alone produce
explicit subtest skips. Subtest names contain provider and benchmark IDs; Go's
`-run` treats slash-separated portions as regular expressions.

## Fixtures

The runner and JSON fixtures are delivered in separate stacked PRs. The runner
can be installed on its own with a custom `BENCHMARK_CONFIG_DIR` or generated
fixtures. The offline catalog check skips when the bundled `testdata` directory
is absent; when it exists, the check requires a valid fixture for every benchmark
in the checked-in provider catalog.

Each checked-in provider benchmark has one file under
`testdata/<provider-id>/<benchmark-id>.json`. Runtime discovery uses the live API
catalog, so a newly discovered benchmark without a file is skipped. Filenames
use Go's `url.PathEscape`, additionally encoding dots as `%2E`. For example,
`inspect/petri-sycophancy` becomes `inspect%2Fpetri-sycophancy.json` under the
`inspect` provider directory. Encoding `%` prevents collisions with escaped IDs.

```json
{
  "parameters": {
    "num_examples": 1,
    "num_fewshot": 0
  },
  "timeout": "5m"
}
```

Options are sent as `benchmarks[].parameters`, matching the API and runtime job
specification. A `limit` or `config` field is not a substitute for those options.
Provider adapters use different option names, so copy a fixture from the same
provider and verify the adapter's supported options before changing it.

Supported fixture fields:

- `parameters`: required object of adapter-specific options.
- `timeout`: required positive Go duration, including submission and polling.
- `test_data_ref`: optional API `TestDataRef` object for staged test input.
- `test_data_ref_env`: alternatively, the name of an environment variable containing
  a complete JSON `TestDataRef` object. It must be set when the fixture is used.
- `hardware_config`: optional per-benchmark hardware profile, queue, or resources.
- `model`: optional complete replacement of the shared model reference.
- `experiment`: create a unique MLflow experiment reference (needed by ATIF).
- `experiment_name`: use a specific MLflow experiment name, either a literal or
  an environment substitution. A nonempty name enables the experiment reference
  and takes precedence over `experiment`, including when that flag is `false`.
- `unbounded`: adapter has no supported sample cap; submission requires
  `BENCHMARK_ALLOW_UNBOUNDED=1` after reviewing the workload.
- `notes`: prerequisites and workload limitations, printed in test output.

`${ENV_NAME}` in any fixture string is replaced with a nonempty environment value.
Substitution preserves JSON escaping, does not recursively expand injected values,
and fails if a required variable is missing. Unknown fixture fields and invalid
API request shapes fail before job submission.

To supply an MLflow experiment name through the environment, add this field to
each fixture that should use it:

```json
{
  "parameters": {"num_examples": 1},
  "timeout": "5m",
  "experiment_name": "${MLFLOW_EXPERIMENT_NAME}"
}
```

```bash
export MLFLOW_EXPERIMENT_NAME=benchmark-smoke
make test-benchmarks
```

The Makefile target also exports a value supplied as a Make variable:

```bash
make test-benchmarks MLFLOW_EXPERIMENT_NAME=benchmark-smoke
```

The environment variable alone does not change fixtures: they must reference it
through `experiment_name`. A missing or empty referenced variable fails before
submission. Multiple benchmark jobs can share the supplied experiment name while
their job names remain unique. With `experiment: true` and no explicit name,
each job still uses its generated unique name. An empty literal
`experiment_name` behaves as if omitted; whitespace-only names are rejected.
The EvalHub service must already have MLflow configured.

Generate missing files after adding provider benchmarks:

```bash
make generate-benchmark-configs
go test ./tests/benchmarks/...
```

The generator reads `config/providers/*.yaml` and preserves existing files, so
reviewed fixture edits survive regeneration. A new provider requires a reviewed
workload profile in `generate/main.go`. The offline catalog test checks fixture
coverage and request validity with synthetic environment values; it does not
certify adapter execution or that staged datasets have the expected contents.

## Provider prerequisites and workload limits

LM Evaluation Harness, LightEval, Garak, Inspect, DataBench, IFBench, and FollowBench
use one example per underlying task or probe. Grouped benchmarks may still expand
to multiple tasks or probes. Some require gated datasets, tokenizers, loglikelihood
endpoints, code sandboxes, or external tools. Inspect custom tasks require
`INSPECT_CUSTOM_TASK`; Bloom custom requires `INSPECT_BLOOM_TEMPLATE`. Petri/Bloom
need capable auditor/judge models. Set sandbox and role-specific options in the
appropriate fixtures for your environment. Garak KFP requires Data Science Pipelines.

GuideLLM limits requests and duration, uses short synthetic prompts, and disables
warmup/cooldown. RULER generates one example at one 4096-token context length.
ToolEval runs one task and requires `TOOL_SERVER_URL` pointing to a reachable
ToolBench server and a model supporting tool calls.

Data-dependent providers require tiny, already staged inputs accessible to the
runtime. Set `RAGAS_TEST_DATA_REF`, `DEEPEVAL_TEST_DATA_REF`, `CLEAR_TEST_DATA_REF`, or
`ATIF_TEST_DATA_REF` as applicable. For example:

```bash
export RAGAS_TEST_DATA_REF='{"s3":{"bucket":"smoke-data","key":"ragas/dataset.jsonl","secret_ref":"smoke-storage"}}'
```

Use an S3, PVC, Git, or Hugging Face data reference supported by EvalHub. References
point to data in the runtime environment, not this test process's filesystem.

- RAGAS: one complete RAG record in `dataset.jsonl`, including metric-required
  fields; the full suite also needs embeddings support.
- DeepEval: one JSONL record containing `input`, `actual_output`, `expected_output`,
  `context`, and `retrieval_context`. It loads the staged dataset without a sample
  cap, so input size bounds the work.
- IBM CLEAR: one short trace in CLEAR's expected format; no sample cap.
- ATIF: one valid one-step ATIF trajectory, plus configured MLflow for artifacts.

MTEB loads an embedding model locally; set `MTEB_MODEL_NAME` to a compatible model.
Fixtures select one representative task per benchmark family, but the adapter has
no supported per-task sample cap. NeMo Guardrails ignores `num_examples` and uses
packaged dataset limits, and needs the relevant guardrail configs/models. Both are
marked `unbounded` and fail before submission unless
`BENCHMARK_ALLOW_UNBOUNDED=1`. A truly small run for these providers needs adapter
changes upstream; the suite never pretends that an ignored parameter bounds work.

## Assertions and cleanup

A pass requires the job to complete, the matching provider/benchmark status to be
`completed`, and a matching result entry. No score threshold or exhaustive metric
schema is asserted. Seeing only `running` is insufficient.

Each job has a unique name. The runner disables client request retries and does
not retry failed evaluations. The API currently has no per-job retry configuration.
Failures report the job ID, elapsed time, and last observed status including
benchmark errors. Registered cleanup hard-deletes the job and its runtime resources
after success, failure, or timeout, using an independent 30-second deadline.
Cleanup errors fail the test. An externally killed process or Go's global test
timeout can interrupt cleanup; set the suite deadline above the expected total
runtime and inspect remaining jobs tagged `benchmark-smoke` after such a run.

To validate the harness without a cluster:

```bash
go test -race ./tests/benchmarks/...
make fmt lint
```

The harness tests exercise discovery pagination, strict fixtures, safe paths and
environment substitution, terminal failures, missing results, timeouts, and
cleanup. Subprocess tests execute the live suite against an HTTP fake to verify
parallel execution across providers, missing-fixture skips, zero-submission
failure, authenticated requests, and cleanup on success/failure/timeout.
