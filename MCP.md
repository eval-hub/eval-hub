# Development tips for MCP

## Creating a MCP service using the evalhub CR

This example shows the minimal config required to enable MCP in the evalhub CR:

```yaml
spec:
  mcp:
    enabled: true
    replicas: 1
```

Once the `evalhub` instance is created the pods should appear in the namespace, something like this:

```shell
NAME                           READY   STATUS    RESTARTS   AGE
evalhub-89f665dff-wk8d6        2/2     Running   0          17m
evalhub-mcp-78b9dff58b-njlcd   2/2     Running   0          17m
```

Note that there are 2 containers because each pod is running its own `kube-rbac-proxy`.

## Authentication

There are two separate authentication layers:

| Layer | What it protects | Configuration |
|-------|------------------|---------------|
| **Inbound (client → evalhub-mcp)** | Who may call the MCP server over HTTP | `auth_type` (see below) |
| **Outbound (evalhub-mcp → eval-hub API)** | Access to providers, jobs, etc. | `EVALHUB_TOKEN`, `EVALHUB_TENANT`, `EVALHUB_BASE_URL` |

Inbound auth applies to **HTTP transports only** (`http`, `http-sse`). The **stdio** transport has no HTTP headers; configure outbound credentials via environment variables on the MCP process instead.

The `/health` endpoint is always unauthenticated.

### `auth_type` values

Set `auth_type` in the evalhub-mcp config file or with environment variable `EVALHUB_AUTH_TYPE`:

| Value | Use case | Client requirement |
|-------|----------|-------------------|
| `none` (default) | Local development, open HTTP listener | No MCP-level auth |
| `rbac-proxy` | OpenShift deployment behind kube-rbac-proxy | `Authorization: Bearer <token>` to the proxy; proxy forwards `X-User` and `X-Tenant` |

### OpenShift: `rbac-proxy`

On cluster, `kube-rbac-proxy` sits in front of `evalhub-mcp` and validates the caller's bearer token. The operator should configure evalhub-mcp with:

```yaml
auth_type: rbac-proxy
```

`evalhub-mcp` then requires these headers on every MCP request (injected by kube-rbac-proxy after successful token review):

- `X-Tenant` — tenant namespace
- `X-User` — authenticated user identity

The `evalhub-mcp` process itself does not validate the bearer token; the sidecar does that before the request arrives.

### Outbound eval-hub API credentials

Regardless of `auth_type`, `evalhub-mcp` needs credentials to call the eval-hub REST API when `base_url` is configured:

```bash
export EVALHUB_BASE_URL="https://<evalhub-api-host>"
export EVALHUB_TOKEN="<your-token>"
export EVALHUB_TENANT="<your-tenant>"
```

For stdio transport (Cursor, VS Code, Claude Code), pass these in the MCP client's `env` block rather than in HTTP headers.

### Configuration reference

YAML keys and environment variables (env overrides YAML):

| Setting | YAML key | Environment variable |
|---------|----------|----------------------|
| Auth mode | `auth_type` | `EVALHUB_AUTH_TYPE` |
| Skip TLS verification (eval-hub API) | `insecure` | `EVALHUB_INSECURE` |
| Eval-hub API URL | `base_url` | `EVALHUB_BASE_URL` |
| Eval-hub token | `token` | `EVALHUB_TOKEN` |
| Eval-hub tenant | `tenant` | `EVALHUB_TENANT` |
| Transport | `transport` | `EVALHUB_TRANSPORT` |
| HTTP host / port | `host`, `port` | `EVALHUB_HOST`, `EVALHUB_PORT` |

CLI flags override YAML and environment variables when set: `--auth-type`, `--transport`, `--host`, `--port`, `--insecure`, `--tls-cert`, `--tls-key`.

Load a config file with `--config /path/to/config.yaml` or `~/.evalhub/config.yaml`.

## MCP capabilities reference

When `EVALHUB_BASE_URL` is configured and the eval-hub API is reachable, the MCP server advertises **tools**, **resources**, **prompts**, and **completions**. Without a backend, only the `evalhub://server/version` resource is available (no tools).

### Tools

| Tool | Parameters | Description |
|------|------------|-------------|
| `discover_providers` | Optional: `target_type` (`model`, `agent`, or `inference_server`); `evaluates` (array of capability tags, e.g. `safety`, `robustness` — provider must evaluate **all** listed values) | Discover evaluation providers with agent-oriented metadata: summary, usage hints, result interpretation, complementary providers, and when to use each provider. Unfiltered calls return every provider; when filters are set, only providers with agent metadata can match. |
| `submit_evaluation` | Required: `name`, `model` (REST `ModelRef` shape: `url`, `name`, optional `parameters`, optional `auth.secret_ref`); either `benchmarks` (REST `EvaluationBenchmarkConfig` list) **or** `collection` (`CollectionRef`), not both. Optional: `description`, `tags`, `experiment` (`name`, `tags`, `artifact_location`) | Submit a new evaluation job. Returns `job_id` and initial `state`. |
| `get_job_status` | Required: `job_id` | Poll job state, progress percentage, and per-benchmark status. Completed benchmarks may include `result_interpretation` and `complements` from provider metadata. |
| `cancel_job` | Required: `job_id` | Cancel a running or pending job. Use `get_job_status` to confirm the final state. |

**Example — discover providers for agent safety evaluation:**

```json
{
  "target_type": "agent",
  "evaluates": ["safety"]
}
```

**Example — submit with a collection:**

```json
{
  "name": "safety-eval",
  "model": {"url": "http://my-model:8080/v1", "name": "my-model"},
  "collection": {"id": "safety-suite"}
}
```

### Resources

All resource URIs use the `evalhub://` scheme and return JSON.

| URI | Description |
|-----|-------------|
| `evalhub://providers` | List evaluation providers (supports `?limit=` and `?offset=` pagination) |
| `evalhub://providers/{id}` | Get a provider by ID |
| `evalhub://benchmarks` | List all benchmarks |
| `evalhub://benchmarks?label={label}` | Filter benchmarks by label (repeat `label` for multiple values) |
| `evalhub://benchmarks/{id}` | Get a benchmark by ID |
| `evalhub://collections` | List benchmark collections (supports `?limit=` and `?offset=`) |
| `evalhub://collections/{id}` | Get a collection by ID |
| `evalhub://jobs` | List evaluation jobs (supports `?limit=` and `?offset=`) |
| `evalhub://jobs?status={status}` | Filter jobs by status: `pending`, `running`, `completed`, `failed`, `cancelled`, `partially_failed` |
| `evalhub://jobs/{id}` | Get a job by ID (full status and per-benchmark progress) |
| `evalhub://server/version` | Server version and build metadata (always available) |

### Prompts

| Prompt | Arguments | Description |
|--------|-----------|-------------|
| `edd_workflow` | Required: `application_type` (`rag`, `agent`, `safety`, or `classifier`) | Evaluation-Driven Development cycle (Define → Measure → Iterate) tailored to the application type |
| `evaluate_model` | Optional: `model_url`, `benchmark_preferences` | Step-by-step model evaluation workflow (model URL, benchmark selection, experiment config, submission, monitoring) |
| `compare_runs` | Optional: `job_ids` (comma-separated) | Compare two or more evaluation jobs: fetch results, compare metrics, summarize findings |

### Completions

Argument completion is available for resource template parameters (provider, benchmark, collection, and job IDs; job `status` values; benchmark `label` tags). Values are cached for 30 seconds.

### Typical workflow

1. Call `discover_providers` (or read `evalhub://providers`) to choose an evaluation provider.
2. Browse `evalhub://benchmarks` or `evalhub://collections` to pick benchmarks or a collection.
3. Call `submit_evaluation` with the model endpoint and selected benchmarks or collection.
4. Poll with `get_job_status` until the job reaches a terminal state.
5. Optionally use the `compare_runs` prompt to compare multiple completed jobs.

## Testing that the MCP service is functioning

### OpenShift (kube-rbac-proxy)

1. Set up a port forward to the MCP service:

   ```shell
   oc port-forward svc/evalhub-mcp 8443:8443
   ```

2. Run the MCP inspector:

   ```shell
   export NODE_TLS_REJECT_UNAUTHORIZED=0
   npx @modelcontextprotocol/inspector
   ```

3. In the UI enter `https://127.0.0.1:8443/sse` as the URL (legacy SSE) or the service root for Streamable HTTP, and in the **Authentication** section add a bearer token from `oc whoami -t`.

   Export `NODE_TLS_REJECT_UNAUTHORIZED` to avoid errors related to self-signed certificates.

### Local development (no inbound auth)

```shell
make build-mcp
EVALHUB_BASE_URL=http://localhost:8080 EVALHUB_TOKEN=token EVALHUB_TENANT=tenant \
  ./bin/evalhub-mcp --transport http --host localhost --port 3001
```

Default `auth_type` is `none`; no bearer token is required to reach the MCP endpoint.
