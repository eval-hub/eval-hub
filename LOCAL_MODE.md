# Running EvalHub in Local Mode

Local mode runs the full EvalHub evaluation pipeline on your workstation without a Kubernetes cluster. It is activated with the `--local` flag and uses the same REST API as cluster mode — see the [API overview](README.md#api-overview) and the [full API reference](https://eval-hub.github.io/eval-hub/) for endpoint documentation.

This is useful for:

- Developing and testing evaluation adapters before deploying to a cluster
- Running evaluations against locally-served models (Ollama, llama.cpp, vLLM, etc.)
- Iterating on benchmark configurations without infrastructure overhead
- Debugging the end-to-end evaluation flow

For a hands-on walkthrough with step-by-step setup instructions and a runnable notebook, see the [local LightEval example](examples/local-lighteval/).

To get just the example directory without cloning the full repository:

```bash
git clone --filter=blob:none --sparse --depth 1 https://github.com/eval-hub/eval-hub.git
cd eval-hub
git sparse-checkout set examples/local-lighteval
```

Then follow the instructions in [`examples/local-lighteval/readme.md`](examples/local-lighteval/readme.md).

## Differences from Cluster Mode

The REST API is identical in both modes — the same endpoints, request bodies, and response schemas apply. The differences are in how the server executes evaluation jobs and which features are available.

| Aspect | Cluster mode | Local mode |
|---|---|---|
| **Job execution** | Kubernetes Jobs (containers) | Host subprocesses (`sh -c "<command>"`) |
| **Authentication** | Enabled (configurable) | Disabled automatically |
| **Multi-tenancy** | Tenant isolation via `X-Tenant` header | Single-tenant only |
| **CORS** | Disabled by default | Enabled (allows Swagger UI at `/docs`) |
| **Sidecar proxy** | Injected into job pods | Not used; adapters call services directly |
| **Init container** | Downloads test data to `/test_data` | Not used |
| **Job scheduling (Kueue)** | Supported via `queue` config | Ignored |
| **Process isolation** | Container sandbox per job | Shared host environment |
| **Provider runtime config** | `runtime.k8s` (image, entrypoint, resources) | `runtime.local` (command, env vars) |

## How Local Job Execution Works

When an evaluation job is submitted in local mode, for each benchmark the server:

1. Writes a **job specification** (`job.json`) to `/tmp/evalhub-jobs/<job_id>/<benchmark_index>/<provider_id>/<benchmark_id>/meta/`
2. Spawns the provider's `runtime.local.command` as a shell process, passing the job spec path via the `EVALHUB_JOB_SPEC_PATH` environment variable
3. Captures stdout/stderr to `jobrun.log` alongside the job spec
4. Tracks subprocess PIDs for cancellation (kills the entire process group on cancel)
5. The adapter reads the job spec, runs the benchmark, and reports results back via the callback URL

```
                      ┌──────────────────────┐
                      │   eval-hub-server     │
                      │   (--local flag)      │
                      ├──────────────────────┤
                      │  Local Runtime        │
                      │  ┌────────────────┐   │
  REST API ──────────►│  │ Write job.json  │   │
  POST /api/v1/       │  │ Spawn process   │   │
  evaluations/jobs    │  │ Track PIDs      │   │
                      │  └───────┬────────┘   │
                      └──────────┼────────────┘
                                 │
                    ┌────────────▼────────────┐
                    │  Adapter Process         │
                    │  (e.g. python main.py)   │
                    │                          │
                    │  Reads EVALHUB_JOB_SPEC_ │
                    │  PATH, runs benchmark,   │
                    │  reports results via      │
                    │  callback URL             │
                    └──────────────────────────┘
                                 │
              ┌──────────────────┼──────────────────┐
              ▼                  ▼                   ▼
        ┌──────────┐      ┌──────────┐        ┌──────────┐
        │  MLflow   │      │   OCI    │        │ eval-hub │
        │  Server   │      │ Registry │        │  (status │
        │(optional) │      │(optional)│        │ callback)│
        └──────────┘      └──────────┘        └──────────┘
```

MLflow and OCI registry are optional — the server and adapters function without them. Configure them only if you need experiment tracking or artifact storage.

### Job file layout

```
/tmp/evalhub-jobs/
└── <job_id>/
    └── <benchmark_index>/
        └── <provider_id>/
            └── <benchmark_id>/
                ├── meta/
                │   └── job.json        # Job specification for the adapter
                └── jobrun.log          # Stdout/stderr from the adapter process
```

### Job specification (job.json)

The job specification is the same structure used in cluster mode (where it is mounted into the container). It contains all the information the adapter needs to run the benchmark:

```json
{
  "id": "<job_id>",
  "provider_id": "<provider_id>",
  "benchmark_id": "<benchmark_id>",
  "benchmark_index": 0,
  "model": {
    "url": "http://localhost:11434/v1",
    "name": "llama3.2:3b-instruct-q4_K_M"
  },
  "num_examples": 10,
  "parameters": {},
  "experiment_name": "my-experiment",
  "tags": [
    { "key": "model", "value": "llama3.2:3b-instruct-q4_K_M" }
  ],
  "callback_url": "http://localhost:8080",
  "exports": {
    "oci": {
      "coordinates": {
        "oci_host": "localhost:5001",
        "oci_repository": "eval-results"
      }
    }
  }
}
```

## Provider Configuration for Local Mode

In local mode, each provider must have a `runtime.local` section specifying the adapter command and optional environment variables. The `runtime.local.command` is executed via `sh -c "<command>"`.

```yaml
id: my-provider
name: My Evaluation Provider
description: Custom evaluation framework adapter
runtime:
  local:
    command: "python main.py"
    env:
      - name: OCI_INSECURE
        value: "true"

benchmarks:
  - id: my_benchmark
    name: My benchmark
    description: Description of what this benchmark measures
    category: reasoning
    metrics:
      - acc
    primary_score:
      metric: acc
      lower_is_better: false
    pass_criteria:
      threshold: 0.25
```

The adapter process receives the following environment variables:

| Variable | Description |
|---|---|
| `EVALHUB_JOB_SPEC_PATH` | Absolute path to the `job.json` file |
| Custom env vars from `runtime.local.env` | Any additional variables defined in the provider config |

A provider configuration can include both `runtime.local` and `runtime.k8s` sections, allowing the same definition to work in both modes.

## Writing Adapter Results for Both Modes

Adapters that need to work in both cluster and local mode should use a common pattern for resolving the output directory. In cluster mode the adapter writes results relative to its own directory; in local mode the job base path is available and results go under it. See the [LightEval adapter](https://github.com/eval-hub/eval-hub-contrib/blob/main/adapters/lighteval/main.py) for a working example:

```python
if self.local_jobs_base_path is not None:
    output_dir = self.local_jobs_base_path / "results"
else:
    output_dir = Path(__file__).parent / "results"
```

This keeps the adapter codebase shared between both runtime modes.

## Troubleshooting

### Adapter process logs

Check the adapter process output in the job log file:

```bash
cat /tmp/evalhub-jobs/<job_id>/<benchmark_index>/<provider_id>/<benchmark_id>/jobrun.log
```

### Server logs

The server logs structured JSON to stderr. Look for `local runtime` messages:

- `local runtime job spec written` — job spec was created successfully
- `local runtime process started` — adapter process was launched with the logged PID and command
- `local runtime benchmark launch failed` — adapter command failed to start

### Common issues

| Symptom | Cause | Fix |
|---|---|---|
| Job fails immediately | Adapter command not found | Verify `runtime.local.command` path and that dependencies are installed |
| Job stays in `running` state | Adapter is not reporting back | Check the adapter logs in `jobrun.log`; verify the callback URL is reachable |
| `provider has no local runtime configured` | Missing `runtime.local` in provider YAML | Add a `runtime.local.command` to the provider configuration |
| MLflow experiment not created | MLflow not configured | Set `MLFLOW_TRACKING_URI` or `mlflow.tracking_uri` in `config.yaml` |
| OCI push fails | Registry not reachable or requires auth | Verify the registry is running and set `OCI_INSECURE=true` for local registries |
