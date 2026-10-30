# EvalHub

[![GO](https://img.shields.io/badge/Made%20with-Go-1f425f?logo=go&logoColor=white)](https://go.dev)
[![GitHub go.mod Go version](https://img.shields.io/github/go-mod/go-version/eval-hub/eval-hub.svg)](https://github.com/eval-hub/eval-hub)
[![Go Reference](https://pkg.go.dev/badge/github.com/eval-hub/eval-hub.svg)](https://pkg.go.dev/github.com/eval-hub/eval-hub)
[![CI](https://github.com/eval-hub/eval-hub/actions/workflows/ci.yml/badge.svg)](https://github.com/eval-hub/eval-hub/actions/workflows/ci.yml)
[![golangci-lint](https://github.com/eval-hub/eval-hub/actions/workflows/golangci-lint.yml/badge.svg)](https://github.com/eval-hub/eval-hub/actions/workflows/golangci-lint.yml)
[![codecov](https://codecov.io/github/eval-hub/eval-hub/graph/badge.svg?token=LHJACCNC9A)](https://codecov.io/github/eval-hub/eval-hub)
[![TrustyAI Operator ConfigMap Sync](https://github.com/eval-hub/eval-hub/actions/workflows/check-trustyai-service-operator-configmap-sync.yml/badge.svg)](https://github.com/eval-hub/eval-hub/actions/workflows/check-trustyai-service-operator-configmap-sync.yml)[![see sync](https://img.shields.io/badge/syncing_docs-here)](#syncing-providers-and-collections-to-the-trustyai-operator)
[![license](https://img.shields.io/badge/License-Apache2.0-blue.svg?plastic)](https://github.com/eval-hub/eval-hub/blob/main/LICENSE)
[![Signed release](https://github.com/eval-hub/eval-hub/actions/workflows/signed-release.yml/badge.svg)](https://github.com/eval-hub/eval-hub/actions/workflows/signed-release.yml)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/eval-hub/eval-hub/badge)](https://scorecard.dev/viewer/?uri=github.com/eval-hub/eval-hub)
[![OpenSSF Best Practices](https://www.bestpractices.dev/projects/13751/badge)](https://www.bestpractices.dev/projects/13751)
[![SDK PyPI Version](https://img.shields.io/pypi/v/eval-hub-sdk?label=SDK%20PyPI%20Version)](https://pypi.org/project/eval-hub-sdk/)
[![SDK Version](https://img.shields.io/github/v/release/eval-hub/eval-hub-sdk?label=SDK)](https://github.com/eval-hub/eval-hub-sdk/releases/latest)

A lightweight REST API service for orchestrating LLM evaluations across multiple backends. Written in Go, it routes evaluation requests to frameworks like lm-evaluation-harness, RAGAS, Garak, and GuideLLM orchestrated via a [complementary SDK](https://github.com/eval-hub/eval-hub-sdk), tracks experiments via MLflow, and runs natively on OpenShift.

## Native TLS

API and MCP HTTP listeners share generic `TLS_REQUIRED`, `TLS_MIN_VERSION`,
`TLS_CIPHERS` and `TLS_CURVES` controls. See the [native TLS contract](docs/native-tls.md)
for certificate configuration, supported settings, proxy-terminated deployments,
client verification and capability limits. This does not change plaintext metrics
or private sidecar backends into HTTPS.

## Architecture

### OpenShift architecture

```mermaid
flowchart TB
    subgraph clients[Clients]
        direction LR
        rest[REST clients]
        agents[MCP clients / agents]
    end

    subgraph openshift[OpenShift cluster]
        direction TB
        proxy[kube-rbac-proxy<br/>authentication and authorization]
        api[EvalHub API<br/>REST routes and handlers]
        mcp[EvalHub MCP server<br/>tools, resources, prompts<br/>stdio or HTTP]
        mlflow[MLflow]
        otelCollector[OpenTelemetry Collector<br/>OTLP endpoint]
        database[(SQL storage<br/>PostgreSQL typical)]
        config[YAML config<br/>providers and collections]
        metrics[Metrics listener<br/>:8081 /metrics]
        prometheus[Prometheus]
        runtime[Kubernetes runtime]
        kubeapi[Kubernetes API]
        subgraph job[Evaluation job pod]
            direction TB
            init[Optional data/source init]
            adapter[Provider adapter]
            sidecar[EvalHub sidecar<br/>API, model, MLflow and OCI proxies]
            init -.-> adapter
            adapter <--> sidecar
        end
    end

    subgraph externalEndpoints[External endpoints]
        direction LR
        model[Model endpoints]
        registry[OCI registry]
    end

    rest -->|REST| proxy
    proxy --> api
    agents -->|MCP| mcp
    mcp -->|EvalHub REST client| proxy
    api --> database
    config --> api
    api --> runtime
    runtime -->|create jobs| kubeapi
    kubeapi --> job
    sidecar -->|status events| proxy
    sidecar --> mlflow
    api -->|tracking and results| mlflow
    sidecar -->|Model traffic| model
    sidecar -->|OCI traffic| registry
    prometheus -->|scrape| metrics
    api -. OTLP .-> otelCollector
    adapter -. OTLP .-> otelCollector
    sidecar -. OTLP .-> otelCollector

    classDef external fill:#f5f7fa,stroke:#64748b,color:#1e293b
    classDef service fill:#e8f2ff,stroke:#3973ac,color:#142b45
    classDef runtimeNode fill:#eef8f1,stroke:#4b8b62,color:#193d26
    class rest,agents,model,registry external
    class proxy,api,mcp,mlflow,otelCollector,database,config,metrics service
    class runtime,kubeapi,init,adapter,sidecar runtimeNode
```

OpenShift API requests pass through kube-rbac-proxy, which supplies the authenticated user and tenant identity. The API creates Kubernetes jobs; each job pod runs a provider adapter and sidecar. The sidecar proxies model, MLflow, OCI, and API callback traffic. Prometheus scrapes the dedicated metrics listener. The MCP server runs inside the cluster as a process separate from the API.

### Local architecture

```mermaid
flowchart TB
    rest[REST clients]
    agents[MCP clients / agents]
    mcp[EvalHub MCP server<br/>tools, resources, prompts<br/>stdio or HTTP]
    prometheus[Prometheus]
    model[Model endpoints]
    mlflow[MLflow<br/>optional]

    subgraph local[Local machine]
        direction TB
        api[EvalHub API<br/>REST routes and handlers]
        database[(SQLite by default<br/>PostgreSQL optional)]
        config[YAML config<br/>providers and collections]
        metrics[Metrics listener<br/>:8081 /metrics]
        runtime[Local runtime]
        adapter[Provider adapter<br/>local process]
        sidecar[Optional local sidecar<br/>API and per-job model proxy]
    end

    rest -->|REST| api
    agents -->|MCP| mcp
    mcp -->|EvalHub REST client| api
    api --> database
    config --> api
    api --> runtime
    runtime -->|launch| adapter
    adapter -->|API callbacks when sidecar is off| api
    adapter -. when enabled .-> sidecar
    sidecar -->|API callbacks| api
    adapter -. direct model calls when sidecar is off .-> model
    sidecar -->|when enabled| model
    api -->|tracking and results| mlflow
    prometheus -->|scrape| metrics

    classDef external fill:#f5f7fa,stroke:#64748b,color:#1e293b
    classDef service fill:#e8f2ff,stroke:#3973ac,color:#142b45
    classDef runtimeNode fill:#eef8f1,stroke:#4b8b62,color:#193d26
    class rest,agents,mcp,prometheus,model,mlflow external
    class api,database,config,metrics service
    class runtime,adapter,sidecar runtimeNode
```

Local mode connects directly to the API without kube-rbac-proxy and runs each adapter as a local process. The shared sidecar is optional; when enabled, it handles API callbacks and per-job model routing. MLflow can be configured for experiment tracking and result export. Prometheus can scrape the separate metrics listener; local mode also exposes `/metrics` on the API port. The MCP server is a separate process and can run near the MCP client or alongside the API.

## Quick start

### Prerequisites

- Go 1.26+
- Make
- Python 3 (for `make test`; used by scripts/grcat for colored output)
- [uv](https://docs.astral.sh/uv/) (manages the Python venv required by `make start-service` and FVT tests; run `make venv` to create it)
- Podman (for container builds)
- Access to an OpenShift or Kubernetes cluster (for deployment)

### Run locally

```bash
make install-deps
make build
./bin/eval-hub
```

Note that in some cases it may be necessary to exclude certain (newer) dependencies,
this can be done as shown below in the `go.mod` file:

```mod
exclude (
  k8s.io/api v0.36.0
)
```

The API is available at `http://localhost:8080`. Verify it is running:

```bash
curl http://localhost:8080/api/v1/health
```

Interactive documentation is served at `/docs`.

### Run in a container

```bash
podman build -t eval-hub:latest -f Containerfile .
podman run --rm -p 8080:8080 eval-hub:latest
```

### Deploy to OpenShift

EvalHub is managed by the [TrustyAI Service Operator](https://github.com/trustyai-explainability/trustyai-service-operator) via a custom resource:

```yaml
apiVersion: trustyai.opendatahub.io/v1alpha1
kind: EvalHub
metadata:
  name: evalhub
  namespace: my-namespace
spec:
  replicas: 1
  env:
    - name: MLFLOW_TRACKING_URI
      value: "http://mlflow:5000"
    - name: EVALHUB_HARDWARE_PROFILES_NAMESPACE
      value: "opendatahub"  # or redhat-ods-applications on RHOAI
```

Apply the CR to your cluster:

```bash
oc apply -f evalhub-cr.yaml
oc get evalhub -n my-namespace    # check status
```

## Post-processing

Submit post-processing jobs with `POST /api/v1/evaluations/post-processing`.
Retrieve or delete them with `GET` or `DELETE` on
`/api/v1/evaluations/post-processing/{post_processing_job_id}`. To use a completed
evaluation as input, put its ID in
`operations.confidence_interval.results_data_ref.eval_job.id` in the request body.
The submission returns a separate post-processing resource ID.

See the [standalone post-processing example](examples/post-processing/README.md)
for a request payload and create, get, and delete commands.

## Local development

```bash
make start-service          # start in background (logs to bin/service.log)
make stop-service           # stop

make test                   # unit tests
make test-fvt               # BDD functional tests (godog)
make test-all               # both
make test-coverage          # generate coverage.html

make lint                   # go vet
make fmt                    # go fmt
```

### Local kind debugging

The local deployment and demo scripts require a container runtime (**podman**
or **Docker**), kind, kubectl, Make, Git, curl, OpenSSL, and Python 3. Install
`jq` as well to use the API demo and curl examples. The runtime must be running
(for podman on macOS, start the podman machine first). These scripts use the
container runtime to build images, run the kind nodes, and manage the local
image registry. Podman is used in preference to Docker when it is installed and
reachable, except under WSL where Docker is preferred (WSL2 podman can pass
`podman info` but fail during kind node startup); set `CONTAINER_RUNTIME`
(for example `make dev-deploy CONTAINER_RUNTIME=docker`) to force a specific
runtime.

Deploy the local EvalHub service, operator, adapters, and mock target/judge
services:

```bash
make dev-deploy
```

By default this creates or reuses the `evalhub-dev` kind cluster and builds the
images from the EvalHub repository plus sibling `../trustyai-service-operator`,
`../eval-hub-contrib`, and `../lm-evaluation-harness` checkouts. The
lm-evaluation-harness checkout is the fork at
<https://github.com/opendatahub-io/lm-evaluation-harness.git> — clone it
alongside EvalHub on its `incubation` (or `stable`) branch, since only those
branches provide the `Dockerfile.lmes-job` used for the LMES adapter image,
not the default `main` branch. The operator
checkout is always required; the contrib and lm-evaluation-harness checkouts
are needed for the adapter images selected by the default `ADAPTERS=all` —
narrow `ADAPTERS` to skip either. Each repository is searched as a sibling of
the EvalHub checkout first and then as `<parent>/<repo>` under each
colon-separated `DEV_REPO_PARENT_PATHS` entry (default `~/code`); set that
variable when the repositories live elsewhere. The command
prints the private kubeconfig path and API URL. It does not change the current
kubectl context; use the private kubeconfig explicitly:

```bash
KIND_CLUSTER_NAME=evalhub-dev
KCFG="${XDG_CONFIG_HOME:-$HOME/.config}/evalhub/kubeconfigs/${KIND_CLUSTER_NAME}.yaml"
KUBECONFIG="$KCFG" kubectl get pods -n system
```

Remove the EvalHub deployment and keep the kind cluster for later use:

```bash
make dev-clean
```

Delete the deployment and the selected cluster as well:

```bash
make dev-clean DELETE_CLUSTER=true
```

Both cleanup modes retain the local build image tags and the container runtime's
build cache.
They remove temporary localhost:<port> tags used only to push images into the
kind registry. Repeated deploys of unchanged EvalHub source reuse the Go build
layers. `make dev-deploy-help` and `make dev-clean-help` list the settings.

Run the API demo against the deployed cluster:

```bash
make dev-demo
```

The demo lists system collections and recent jobs, clones
`instruction-output-v1` with a single FollowBench benchmark, submits a one-example
job to the local mock target and judge, and polls until the job completes. It
mints a short-lived token for the local EvalHub service account unless `API_TOKEN`
is supplied. It creates a Secret containing only the mock services' test keys;
no external model credentials are used. The cloned collection and job remain for
inspection. The demo Secret is removed by `make dev-clean`.

To use a different system collection as the clone source or change the polling
timeout:

```bash
make dev-demo DEMO_SOURCE_COLLECTION_ID=instruction-output-v1 DEMO_JOB_TIMEOUT_SECONDS=900
make dev-demo-help
```

Run the benchmark smoke tests from `tests/benchmarks` against the deployed
cluster:

```bash
make test-benchmarks-kind
```

This deploys (or reuses) the kind cluster via `make dev-deploy` — pass
`SKIP_BUILD=true` to reuse previously built images — mints a short-lived token
for the local EvalHub service account, and points the suite at the loopback API
URL with the in-cluster mock target as the model. By default it runs only the
providers whose adapter images the deployment builds (Inspect, FollowBench, and
LM Evaluation Harness); the remaining benchmark-test inputs are unchanged:

```bash
make test-benchmarks-kind BENCHMARK_RUN='TestBenchmarkSmoke/inspect/'
make test-benchmarks-kind SKIP_BUILD=true BENCHMARK_TIMEOUT=1h
```

The mock-auth Secret it creates is the same one `make dev-demo` uses, and
`make dev-clean` removes it.

#### Calling the API with curl

These commands use the local self-signed TLS certificate. Create a short-lived
token and keep it in a private curl config file so the token is not placed in
curl's process arguments:

```bash
KIND_CLUSTER_NAME=evalhub-dev
KCFG="${XDG_CONFIG_HOME:-$HOME/.config}/evalhub/kubeconfigs/${KIND_CLUSTER_NAME}.yaml"
API_BASE=https://127.0.0.1:18443
API_TOKEN="$(kubectl --kubeconfig "$KCFG" -n system create token evalhub-local-service --duration=1h)"
CURL_CONFIG="$(mktemp)"
chmod 600 "$CURL_CONFIG"
printf 'header = "Authorization: Bearer %s"\nheader = "X-Tenant: system"\nheader = "Content-Type: application/json"\n' "$API_TOKEN" >"$CURL_CONFIG"
unset API_TOKEN
trap 'rm -f -- "$CURL_CONFIG"' EXIT
```

Check health and list collections and jobs:

```bash
curl --silent --show-error --insecure "$API_BASE/api/v1/health" | jq .
curl --silent --show-error --insecure --config "$CURL_CONFIG" \
  "$API_BASE/api/v1/evaluations/collections?scope=system&limit=100" | jq .
curl --silent --show-error --insecure --config "$CURL_CONFIG" \
  "$API_BASE/api/v1/evaluations/jobs?limit=20" | jq .
```

The demo Secret can be created directly if you want to run the clone/job calls
without `make dev-demo`. These are fake keys accepted only by the local mocks:

```bash
kubectl --kubeconfig "$KCFG" -n system create secret generic evalhub-kind-demo-auth \
  --from-literal=api-key=target-test-token \
  --from-literal=judge_api-key=judge-test-token \
  --from-literal=judge_url=http://evalhub-kind-mock-judge.system.svc.cluster.local:8000/v1 \
  --dry-run=client -o yaml | kubectl --kubeconfig "$KCFG" apply -f -
kubectl --kubeconfig "$KCFG" -n system annotate secret evalhub-kind-demo-auth \
  "io.evalhub.kind-smoke.cluster=$KIND_CLUSTER_NAME" --overwrite
```

Clone the system collection, narrowed to one FollowBench example:

```bash
CLONE_JSON="$(curl --silent --show-error --insecure --config "$CURL_CONFIG" \
  --data-binary '{"name":"kind-curl-demo","benchmarks":[{"id":"followbench","provider_id":"followbench","parameters":{"judge_model":"kind-judge-model","num_examples":1,"max_tokens":64,"temperature":0,"request_timeout":60}}]}' \
  "$API_BASE/api/v1/evaluations/collections/instruction-output-v1/clones")"
CLONE_ID="$(jq -r '.resource.id' <<<"$CLONE_JSON")"
printf 'Created collection %s\n' "$CLONE_ID"
```

Submit the job using the cloned collection and mock models, then retrieve its
status using the returned job ID:

```bash
JOB_JSON="$(curl --silent --show-error --insecure --config "$CURL_CONFIG" \
  --data-binary @- "$API_BASE/api/v1/evaluations/jobs" <<JSON
{
  "name": "kind-curl-demo-job",
  "model": {
    "url": "http://evalhub-kind-mock-target.system.svc.cluster.local:8000/v1",
    "name": "kind-target-model",
    "auth": {"secret_ref": "evalhub-kind-demo-auth"}
  },
  "collection": {
    "id": "$CLONE_ID",
    "benchmarks": [{
      "id": "followbench",
      "provider_id": "followbench",
      "parameters": {
        "judge_model": "kind-judge-model",
        "num_examples": 1,
        "max_tokens": 64,
        "temperature": 0,
        "request_timeout": 60
      }
    }]
  }
}
JSON
)"
JOB_ID="$(jq -r '.resource.id' <<<"$JOB_JSON")"
printf 'Created job %s\n' "$JOB_ID"
curl --silent --show-error --insecure --config "$CURL_CONFIG" \
  "$API_BASE/api/v1/evaluations/jobs/$JOB_ID" | jq '{id: .resource.id, name, state: .status.state}'
rm -f -- "$CURL_CONFIG"
```

For API request and response details, see the [OpenAPI reference](docs/src/openapi.yaml).

For local post-processing, the runtime launches the real adapter from a sibling
`../eval-hub-contrib/adapters/evalhub-post-processor` checkout. Install that
adapter's `requirements.txt` in its `.venv` before starting EvalHub from this
repository root. For another checkout or Python environment, set
`EVALHUB_POST_PROCESSING_LOCAL_COMMAND` to the command that runs its `main.py`.
The adapter also needs a reachable callback service and access to the referenced
data; see the adapter's README for local setup.

Run a single test:

```bash
go test -v ./internal/handlers -run TestHandleName
```

To create a Python wheel distribution of the server for local development and testing:

```sh
make cross-compile
make build-wheel
```

### Quick local OCI evaluation card check

Developers can use `scripts/test-local-oci-export.sh` to simulate a quick OCI evaluation card
generation during development. It runs an evaluation with the local test adapter,
downloads the exported card from a local OCI registry, and verifies it against
the completed job. No model server is required.

Install these dependencies before starting:

- `curl`, `jq`, and `oras`, available on your `PATH`.
- Go (the version specified in `go.mod`) and `uv` for the service setup.
- A running container engine: Podman by default, or Docker with `DOCKER=docker`.

From the repository root, start the registry and service:

```sh
make start-oci-registry
make start-service
```

These targets start the registry at `localhost:5001` and build and start the
service in local mode at `http://localhost:8080`, including the test adapter setup.
For Docker, use `make start-oci-registry DOCKER=docker`.

Run the check:

```sh
bash scripts/test-local-oci-export.sh
```

A successful run prints a verification message and saves these files in
`./bin/local-oci/` by default:

| File | Contents |
| --- | --- |
| `request.json` | Submitted evaluation job request |
| `created.json` | Job creation response, including the job ID |
| `job.json` | Latest job status response, with results on completion |
| `manifest.json` | Exported OCI artifact manifest |
| `artifact-config.json` | OCI artifact configuration |
| `evaluation-card-<job-id>.json` | Downloaded evaluation card |

Set `OUTPUT_DIR` to save verification files elsewhere:

```sh
OUTPUT_DIR=/tmp/local-oci-check bash scripts/test-local-oci-export.sh
```

When finished, stop the service and remove the registry container:

```sh
make stop-service
make stop-oci-registry
```

If using Docker, use `make stop-oci-registry DOCKER=docker`.

### Exposing private functions for tests

Create a file called `export_test.go` in the package under test and re-export symbols needed by `_test.go` files in other packages.

### Database

SQLite in-memory is the default (`database.driver: sqlite` in `config/config.yaml`). To use PostgreSQL locally there are two approaches: a container or a native install. Both use targets in `tests/postgres/Makefile`.

> **Note:** The credentials and auth settings below are for local development and testing only. For production deployments, use strong passwords, TLS, and appropriate authentication mechanisms.

#### Option 1: Container (Podman/Docker)

No system-level install required. The container creates the database, user, and permissions automatically.

```bash
cd tests/postgres
POSTGRES_PASSWORD=<your-password> make start-postgres-container
```

To stop and remove:

```bash
cd tests/postgres
make stop-postgres-container
make delete-postgres-container
```

Configure EvalHub in `config/config.yaml`:

```yaml
database:
  driver: pgx
  url: postgres://eval_hub:<your-password>@localhost:5432/eval_hub
```

Or override via environment variables:

```bash
export DB_DRIVER=pgx
export DB_URL="postgres://eval_hub:<your-password>@localhost:5432/eval_hub"
```

#### Option 2: Native install (Homebrew on macOS, apt on Linux)

```bash
cd tests/postgres
make install-postgres
make start-postgres
make create-user
make create-database
make grant-permissions
```

To stop:

```bash
cd tests/postgres
make stop-postgres
```

Configure EvalHub in `config/config.yaml` (no password needed with trust/peer auth):

```yaml
database:
  driver: pgx
  url: postgres://eval_hub@localhost:5432/eval_hub
```

Or override via environment variables:

```bash
export DB_DRIVER=pgx
export DB_URL="postgres://eval_hub@localhost:5432/eval_hub"
```

## Configuration

Configuration is loaded from `config/config.yaml`, overridden by environment variables and secret files.

| Variable | Purpose | Default |
| --- | --- | --- |
| `PORT` | API listen port | `8080` |
| `DB_DRIVER` | Database driver (`sqlite` or `pgx`) | `sqlite` |
| `DB_URL` | Database connection string | SQLite in-memory |
| `MLFLOW_TRACKING_URI` | MLflow tracking server | `http://localhost:5000` |
| `MLFLOW_CA_CERT_PATH` | PEM CA bundle for MLflow TLS verification | (system roots) |
| `LOG_LEVEL` | Logging level | `INFO` |
| `EVALHUB_HARDWARE_PROFILES_NAMESPACE` | Platform namespace where OpenDataHub `HardwareProfile` CRs are fetched (Kubernetes runtime). Required for `hardware_config.hardware_profile_name` evaluations; typically `opendatahub` or `redhat-ods-applications`. Set by the TrustyAI Service Operator deployment. | _(unset — hardware profile lookups fail)_ |

Provider configurations live in `config/providers/` as YAML files. The default set includes lm-evaluation-harness (167 benchmarks), RAGAS, Garak, GuideLLM, LightEval, and MTEB.

### Syncing providers and collections to the TrustyAI operator

Provider and collection definitions are maintained here and mirrored as ConfigMaps in the [TrustyAI Service Operator](https://github.com/trustyai-explainability/trustyai-service-operator):

- Providers: `config/providers/` → `config/configmaps/evalhub/provider-*.yaml`
- Collections: `config/collections/` → `config/configmaps/evalhub/collection-*.yaml`

When adding or changing a provider or collection, update the source YAML in this repository and the corresponding embedded ConfigMap in the operator repository. Add new ConfigMaps to the operator's `config/configmaps/evalhub/kustomization.yaml`. Keep the two repositories' changes coordinated so the operator can deploy the same definitions.

The sync check compares the embedded ConfigMap YAML with the source files. Run it locally with:

```bash
python scripts/check_configmap_sync.py
```

The same check runs in CI through the [TrustyAI Operator ConfigMap Sync workflow](.github/workflows/check-trustyai-service-operator-configmap-sync.yml).

## API overview

All endpoints are versioned under `/api/v1`. Full specification at [eval-hub.github.io/eval-hub](https://eval-hub.github.io/eval-hub/).

| Endpoint | Methods | Description |
| --- | --- | --- |
| `/api/v1/evaluations/jobs` | POST, GET | Create or list evaluation jobs |
| `/api/v1/evaluations/jobs/{id}` | GET, DELETE | Get status or cancel a job |
| `/api/v1/evaluations/collections` | GET, POST | List or create benchmark collections |
| `/api/v1/evaluations/providers` | GET, POST | List or create providers |
| `/api/v1/evaluations/providers/{id}` | GET, PUT, PATCH, DELETE | Manage a provider |
| `/api/v1/evaluations/jobs/{id}/events` | POST | Submit job events |
| `/api/v1/health` | GET | Health check (no identity headers; no build/version fields) |
| `/metrics` | GET | Prometheus metrics |

Detailed API documentation: [eval-hub.github.io/eval-hub](https://eval-hub.github.io/eval-hub/)

## Custom backends

EvalHub supports Bring Your Own Framework (BYOF). Extend the `FrameworkAdapter` class from the [eval-hub-sdk](https://github.com/eval-hub/eval-hub-sdk) and implement a single method -- EvalHub handles scheduling, status reporting, and result aggregation.

```python
from evalhub.adapter import FrameworkAdapter, JobSpec, JobCallbacks, JobResults, EvaluationResult

class MyAdapter(FrameworkAdapter):
    def run_benchmark_job(self, config: JobSpec, callbacks: JobCallbacks) -> JobResults:
        # run your evaluation logic, report progress via callbacks
        callbacks.report_status(JobStatusUpdate(status=JobStatus.RUNNING, progress=0.5))
        score = evaluate(config.model, config.parameters)
        return JobResults(
            id=config.id,
            benchmark_id=config.benchmark_id,
            model_name=config.model.name,
            results=[EvaluationResult(metric_name="accuracy", metric_value=score)],
            num_examples_evaluated=100,
            duration_seconds=elapsed,
        )
```

Register the new provider by adding a YAML entry to the providers ConfigMap. No additional services or TCP listeners are required -- adapters run as jobs, not servers. Once registered, the provider and its benchmarks are available through the standard `/api/v1/evaluations/providers` endpoint.

## Project structure

```text
eval-hub/
├── cmd/eval_hub/          # Entry point (main binary)
├── internal/
│   ├── handlers/          # HTTP request handlers
│   ├── storage/           # Database abstraction (SQLite, PostgreSQL)
│   ├── mlflow/            # MLflow client
│   ├── runtimes/          # Backend execution adapters
│   ├── config/            # Viper-based configuration
│   ├── validation/        # Request validation
│   ├── metrics/           # Prometheus instrumentation
│   └── logging/           # Structured logging (zap)
├── config/                # config.yaml and provider definitions
├── docs/src/              # OpenAPI 3.1.0 specification (source of truth)
├── tests/features/        # BDD tests (godog)
├── Containerfile          # Multi-stage UBI9 container build
└── Makefile               # Build, test, and dev targets
```

## Local mode

EvalHub can run evaluations locally without a Kubernetes cluster. See the [local mode guide](https://eval-hub.github.io/guides/local-mode/) for configuration, architecture details, and troubleshooting, and the [local mode tutorial](https://eval-hub.github.io/guides/local-mode-tutorial/) for a step-by-step walkthrough. A self-contained [LightEval example](examples/local-lighteval/) is included in this repository.

When local mode has `mlflow.tracking_uri` configured, each local evaluation subprocess automatically receives that direct URI as `MLFLOW_TRACKING_URI`. Local subprocess environment variables are applied in this order, with later values replacing matching earlier values:

1. Inherited process environment
2. EvalHub and service configuration values
3. Provider `runtime.local.env` values

## Further reading

- [API documentation](https://eval-hub.github.io/eval-hub/) -- full endpoint reference
- [Local mode guide](https://eval-hub.github.io/guides/local-mode/) -- running evaluations without Kubernetes
- [CONTRIBUTING.md](./CONTRIBUTING.md) -- contribution guidelines
- [OpenAPI spec](./docs/openapi.yaml) -- machine-readable API definition

## Licence

Apache 2.0 -- see [LICENSE](./LICENSE).
