# Feature Tests

This directory contains Cucumber/Gherkin feature tests for the eval-hub using the [godog](https://github.com/cucumber/godog) framework.

## Test Execution Modes

The tests support two execution modes:

### Remote Server Mode

When the `SERVER_URL` environment variable is set, the tests will run against a remote server instead of starting a local instance.

```bash
export SERVER_URL="https://api.example.com"
```

The `SERVER_URL` should be a fully qualified URL (e.g., `http://localhost:8080` or `https://api.example.com`).

If the remote server requires authentication, set:

```bash
export AUTH_TOKEN="your-token"
export X_TENANT="tenant-namespace"
```

### Model Overrides (Required)

Set the model fields in the test payloads using environment variables:

- `MODEL_URL` (defaults to `http://test.com`)
- `MODEL_NAME` (defaults to `test`)

Example:

```bash
export MODEL_URL="http://granite-llm-metrics.prabhu.svc.cluster.local:8080/v1"
export MODEL_NAME="granite-llm"
```

Run all feature tests:

```bash
go test ./tests/features/...
```

### Local Server Mode (Default)

If `SERVER_URL` is not set, the tests will automatically start the server in a separate goroutine before running the test suite. The server will be started on:

- Port `8080` by default, or
- The port specified by the `PORT` environment variable

```bash
# Use default port 8080
go test ./tests/features/...

# Use custom port
export PORT=9090
go test ./tests/features/...
```

When running in local server mode, the tests will:

1. Start the server in a background goroutine during test suite initialization
2. Wait for the server to be ready by checking the health endpoint
3. Automatically shut down the server after all tests complete

## Test Structure

- **Feature files** (`.feature`): Gherkin syntax test scenarios
- **Step definitions** (`step_definitions_test.go`): Implementation of test steps
- **Test suite** (`suite_test.go`): Test suite configuration and initialization

## Test tags

| Tag | Use |
| --- | :-- |
| `@collections` | Used to run just the collections tests |
| `@evaluations` | Used to run just the evaluations tests |
| `@providers` | Used to run just the providers tests |
| `@mcp` | MCP tool and resource scenarios in `mcp.feature` |
| `@cluster` | Tests that require the Kubernetes cluster runtime (default `make test-fvt` excludes them via `~@cluster` in `FVT_TAGS`) |
| `@local_runtime` | Scenarios that require a **fully functional local evaluation runtime**—eval-hub in local mode (embedded FVT server with `LocalMode`, or a binary started with `--local`). Use for flows such as local evaluation jobs that run to completion. Distinct from `@cluster`. **Excluded by default for `make test-fvt`/CI** via `~@local_runtime` in Makefile `FVT_TAGS`; `suite_test.go` only defaults to `~@ignore`, so plain `go test ./tests/features/...` does not exclude `@local_runtime` unless you set `GODOG_TAGS` |
| `@local` | Still used on some evaluation scenarios in `evaluations.feature` for local (non-cluster) job flows; prefer `@local_runtime` for new scenarios that depend on full local job/runtime execution |
| `@gpu` | GPU resource management scenarios in `gpu_resources.feature`; requires a cluster with GPU test setup (see `GPU_TESTING.md`) |
| `@kueue` | Scenarios that require Kueue queue integration (`evaluation_jobs.feature`, `gpu_resources.feature`) |
| `@negative` | Used to mark this as a negative test |
| `@mlflow` | Tests that only work when running with a configured mlflow service |
| `@slow` | Tests that take more than the normal timeout (currently 1 hour) |
| `@ignore` | Can be used to ignore a test |
| `@connected` | Used by the Jenkins jobs and set when running on a connected cluster |
| `@disconnected` | Used by the Jenkins jobs and set when running on a disconnected cluster |
| `@hardware_profile` | Hardware profile API and Kubernetes Job adapter resource tests in `evaluation_jobs.feature`; require pipeline env vars (see below). |
| `@metrics` | Prometheus `/metrics` scrape tests in `metrics.feature`; in cluster/remote mode require `METRICS_URL` (see below). |
| `@logs` | Evaluation job log collection scenarios in `evaluation_local_jobs.feature` and `evaluation_jobs.feature` |
| `@pvc` | Evaluation jobs that mount offline test data from a PersistentVolumeClaim (`evaluation_jobs.feature`). Defaults: claim `evalhub-offline-test-data`, `sub_path` `staging`. Override with `TEST_DATA_PVC_CLAIM_NAME` / `TEST_DATA_PVC_SUB_PATH`. Missing-PVC negative case uses `TEST_DATA_PVC_MISSING_CLAIM_NAME` (default `evalhub-offline-test-data-does-not-exist`) and waits up to 2m30s for operator failure sync. Opt in with `GODOG_TAGS="@pvc"`. |
| `@gha-wheel-sanity` | Local-runtime wheel validation scenario run by `scripts/gha_wheel_sanity_test.sh` during GHA wheel checks |

### Metrics tests (`@metrics`)

Prometheus metrics are served on a **dedicated port** (default `8081`) and are scraped directly, not through kube-rbac-proxy. API requests continue to use `SERVER_URL`; scrape requests use `METRICS_URL`.

| Mode | `METRICS_URL` |
| --- | --- |
| Local embedded server (default) | Optional; defaults to the API base URL (`http://localhost:8080` or `PORT`) because local mode serves `/metrics` on the main router |
| Remote / cluster (`SERVER_URL` set) | **Required** — e.g. `http://evalhub-metrics.evalhub.svc.cluster.local:8081` |

```bash
export SERVER_URL="https://evalhub.example.com"
export METRICS_URL="http://evalhub-metrics.evalhub.svc.cluster.local:8081"
export AUTH_TOKEN="..."
export X_TENANT="tenant"
GODOG_TAGS="@metrics" go test -v ./tests/features/...
```

If `SERVER_URL` is set but `METRICS_URL` is not, `@metrics` scenarios are **skipped**.

### Hardware profile tests (`@hardware_profile`)

These scenarios validate that evaluation job APIs accept and persist `hardware_config.hardware_profile_ref`, and that the created Kubernetes Job adapter container receives CPU/memory from the referenced profile. They do **not** create `HardwareProfile` CRs or fetch profile specs in the test binary — the pipeline supplies the profile name and expected adapter resources via environment variables.

**Pipeline / cluster prerequisites** (outside the test binary):

1. Confirm `hardwareprofiles.infrastructure.opendatahub.io` CRD is installed (e.g. `oc get crd hardwareprofiles.infrastructure.opendatahub.io`).
2. Ensure a `HardwareProfile` exists in the tenant namespace (`X_TENANT`).
3. Export its name and expected adapter resources (must match the profile's `defaultCount` / `maxCount` for CPU and memory):

```bash
export TEST_HARDWARE_PROFILE="your-profile-name"
export TEST_HARDWARE_PROFILE_CPU_REQUEST="1"
export TEST_HARDWARE_PROFILE_MEMORY_REQUEST="1Gi"
export TEST_HARDWARE_PROFILE_CPU_LIMIT="2"
export TEST_HARDWARE_PROFILE_MEMORY_LIMIT="2Gi"
```

4. Grant the FVT test runner **`get`/`list` on `jobs`** in the tenant namespace (to inspect the adapter container). Hardware profile steps use a FVT Kubernetes client that **prefers `KUBECONFIG`** (pipeline `oc login`) over in-cluster credentials, then falls back to in-cluster config for local runs inside the cluster. The test process does not read `HardwareProfile` CRs or cluster-scoped CRDs.

If any required env var above is unset, `@hardware_profile` scenarios are **skipped** (default `make test-fvt` behavior).

Run only hardware profile scenarios against a remote server:

```bash
export SERVER_URL="https://evalhub.example.com"
export AUTH_TOKEN="..."
export X_TENANT="tenant"
export TEST_HARDWARE_PROFILE="fvt-hardware-profile"
export TEST_HARDWARE_PROFILE_CPU_REQUEST="1"
export TEST_HARDWARE_PROFILE_MEMORY_REQUEST="1Gi"
export TEST_HARDWARE_PROFILE_CPU_LIMIT="2"
export TEST_HARDWARE_PROFILE_MEMORY_LIMIT="2Gi"
GODOG_TAGS="@hardware_profile" go test -v ./tests/features/...
```

Note that if you want to run a single test you can add a tag to the test,
such as `@focus` and then set the environment variable `GODOG_TAGS`:

```shell
export GODOG_TAGS=@focus
make clean test-fvt-server
```

To **include** `@local_runtime` scenarios while keeping other default exclusions, override the tag expression (drop `~@local_runtime`). With Make, `FVT_TAGS` is passed through to `go test` and overrides the Makefile default when set in the environment:

```shell
FVT_TAGS='--godog.tags=~@ignore && ~@mlflow && ~@cluster' make test-fvt-server
```

Or set `GODOG_TAGS` for a plain `go test ./tests/features/...` run (see `suite_test.go`; this replaces the built-in default expression entirely).

## Running Tests

### Using Make

The recommended way to run the feature tests is using the Make target:

```bash
make test-fvt
```

or run the FVT tests against a running server,
the make target will run up the server and stop it after the tests,
this target is useful when you want to look at the service logs
which will be stored in the `bin` directory:

```bash
make test-fvt-server
```

This runs the tests with verbose output enabled.

Generate the FVT HTML report (requires Node dev deps):

```bash
npm install
make fvt-report
```

### Using Go Test Directly

Run all feature tests:

```bash
go test ./tests/features/...
```

Run with verbose output:

```bash
go test -v ./tests/features/...
```

Run a specific feature:

```bash
go test -v ./tests/features/... -run TestFeatures -godog.paths=tests/features/evaluations.feature
```
