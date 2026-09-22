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

For APIs that present a private or cluster CA (for example OpenShift service CA), set `ca_cert_path` / `EVALHUB_CA_CERT_PATH` to a PEM bundle. TLS certificate verification is always enabled; there is no skip-verify option.

For stdio transport (Cursor, VS Code, Claude Code), pass these in the MCP client's `env` block rather than in HTTP headers.

### Configuration reference

YAML keys and environment variables (env overrides YAML):

| Setting | YAML key | Environment variable |
|---------|----------|----------------------|
| Auth mode | `auth_type` | `EVALHUB_AUTH_TYPE` |
| Eval-hub API URL | `base_url` | `EVALHUB_BASE_URL` |
| Eval-hub token | `token` | `EVALHUB_TOKEN` |
| Eval-hub tenant | `tenant` | `EVALHUB_TENANT` |
| Custom CA for eval-hub API TLS | `ca_cert_path` | `EVALHUB_CA_CERT_PATH` |
| Transport | `transport` | `EVALHUB_TRANSPORT` |
| HTTP host / port | `host`, `port` | `EVALHUB_HOST`, `EVALHUB_PORT` |

CLI flags override YAML and environment variables when set: `--auth-type`, `--transport`, `--host`, `--port`, `--tls-cert`, `--tls-key`.

Load a config file with `--config /path/to/config.yaml` or `~/.evalhub/config.yaml`.

## MCP capabilities reference

When `EVALHUB_BASE_URL` is configured and the eval-hub API is reachable, the MCP server advertises **tools**, **resources**, **prompts**, and **completions**. Without a backend, only the `evalhub://server/version` resource is available (no tools).

### Tools

| Tool | Parameters | Description |
|------|------------|-------------|
| `discover_providers` | Optional: `target_type` (`model`, `agent`, or `inference_server`); `evaluates` (array of capability tags, e.g. `safety`, `robustness` — provider must evaluate **all** listed values) | Discover evaluation providers with agent-oriented metadata: summary, usage hints, result interpretation, complementary providers, and when to use each provider. Unfiltered calls return every provider; when filters are set, only providers with agent metadata can match. Returns provider-level metadata only — use `search_benchmarks`/`get_benchmark` to obtain benchmark IDs. |
| `search_benchmarks` | Optional: `query` (substring over id/name/description/tags), `labels` (array of tags — benchmark must have **all**), `category`, `provider_id`, `limit` (default 50) | Search the live benchmark catalog. Returns matching benchmarks with their `id` and owning `provider_id` (plus name, category, metrics, thresholds). Use this to find valid benchmark IDs for `create_collection` and `submit_evaluation`. |
| `get_benchmark` | Required: `benchmark_id`. Optional: `provider_id` (disambiguates when the same ID is offered by multiple providers) | Look up a single benchmark by ID. Returns full detail including the `provider_id` required to run it, metrics, `primary_score`, and `pass_criteria`. If the ID is shared by multiple providers and `provider_id` is omitted, returns an ambiguity error listing the providers. |
| `design_collection` | Required: `evaluation_goal`. Optional: `provider_filter`, `max_benchmarks` (default 12), `strictness` (`lenient`, `moderate`, `strict`) | Design a benchmark collection from a natural-language evaluation goal. Fetches the live benchmark catalog and curated collection examples and returns calibration guidance for benchmark selection, weighting, and threshold setting. Returns guidance only — use `create_collection` to persist the design. Model-callable equivalent of the `design_collection` prompt (use the tool when driving the workflow conversationally; the prompt is user-invoked and cannot take multi-word arguments via slash commands). |
| `submit_evaluation` | Required: `name`, `model`; either `benchmarks` **or** `collection` (not both). Optional: `description`, `tags`, `experiment`. Request fields match the eval-hub HTTP API (`POST /api/v1/evaluations`). See **submit_evaluation request shape** below. | Submit a new evaluation job. Returns `job_id` and initial `state`. |
| `get_job_status` | Required: `job_id` | Poll job state, progress percentage, and per-benchmark status. Completed benchmarks may include `result_interpretation` and `complements` from provider metadata. |
| `create_collection` | Required: `name`, `benchmarks`, `domains` (non-empty array). Optional: `description`, `tags`, `pass_criteria`. | Create a new benchmark collection classified by `domains`. Use after designing a collection (via the `design_collection` prompt or manually) to persist it. The collection can then be referenced in `submit_evaluation`. |
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

#### submit_evaluation request shape

`submit_evaluation` accepts the same JSON structure as creating an evaluation job via the HTTP API. You do not need to learn separate MCP field names.

**`model`** (required) — the model endpoint to evaluate:

| Field | Required | Description |
|-------|----------|-------------|
| `url` | yes | Model inference endpoint URL |
| `name` | yes | Display name for the model |
| `parameters` | no | Key/value inference settings (for example `temperature`, `max_tokens`) |
| `auth` | no | Authentication; when set, use `auth.secret_ref` for the Kubernetes secret name |

**`benchmarks`** or **`collection`** (one required, not both):

- **`benchmarks`** — array of benchmarks to run. Each entry requires `id` and `provider_id`. Optional fields include `parameters`, `weight`, `pass_criteria`, and others supported by the HTTP API.
- **`collection`** — object with `id` set to a collection name (for example `safety-suite`). Optionally include a `benchmarks` array to override collection defaults.

**Example — submit with explicit benchmarks and model auth:**

```json
{
  "name": "bench-eval",
  "model": {
    "url": "http://my-model:8080/v1",
    "name": "my-model",
    "parameters": {"temperature": 0.7},
    "auth": {"secret_ref": "my-model-credentials"}
  },
  "benchmarks": [
    {"id": "hellaswag", "provider_id": "lighteval", "parameters": {"num_fewshot": 5}}
  ]
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
| `design_collection` | Required: `evaluation_goal`. Optional: `provider_filter`, `max_benchmarks`, `strictness` (`lenient`, `moderate`, `strict`) | Design a benchmark collection from a natural-language evaluation goal. Dynamically fetches the live benchmark catalog and curated collection examples, then provides calibration guidelines for benchmark selection, weighting, and threshold setting. Use `create_collection` to persist the result. |
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

### Collection design workflow

Use the `design_collection` prompt (user-invoked) or the `design_collection` tool (model-invoked) to generate a benchmark collection from a natural-language goal. The tool is the better fit when an agent drives the workflow from a conversational request, since MCP prompt slash commands cannot pass multi-word argument values; the prompt is best for explicit, interactive invocation. Both share the same catalog and calibration guidance.

1. Invoke `design_collection` with your `evaluation_goal` (e.g. "enterprise deployment requiring safety and instruction following").
2. The agent receives the full benchmark catalog and calibration guidelines, then proposes a collection.
3. Iterate conversationally ("add more safety benchmarks", "lower the thresholds").
4. When satisfied, use the `create_collection` tool to persist the collection in eval-hub.
5. Use the created collection ID with `submit_evaluation` to run evaluations.

**Example — design and create a collection:**

```text
# Step 1: invoke the prompt
design_collection(evaluation_goal="enterprise deployment requiring safety, instruction following, and long-context support")

# Step 2: agent proposes a collection, user approves

# Step 3: persist it
create_collection(name="enterprise-safety-v1", domains=["safety"], benchmarks=[...], pass_criteria={threshold: 0.72})
```

**Other examples:**

```text
I need to evaluate a 7B instruction-tuned model for enterprise deployment — it must be safe, follow instructions well, and handle documents up to 64k tokens
```

```text
I'm fine-tuning a 13B model for customer support. I need to make sure it doesn't produce toxic content, follows instructions accurately, and gives truthful answers
```

In MCP client usage, you'd invoke it like:

```shell
design_collection(evaluation_goal="enterprise deployment requiring safety, instruction following, and long-context support")
```

The prompt handler takes that free text goal, fetches the live benchmark catalog and system collection examples from the API, and returns a rich prompt with calibration guidelines, domain-signal mapping, and threshold ranges — so the AI agent can reason over all of it and propose a complete CollectionConfig.

Optional parameters let you constrain the output:

- provider_filter — e.g. "lm_evaluation_harness,lighteval" to restrict to specific providers
- max_benchmarks — cap on benchmark count (default 12)
- strictness — "lenient", "moderate" (default), or "strict" to shift all thresholds

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

### Local testing

```shell
make start-service
make start-mcp
```

Now the MCP service will be running on `http://localhost:3001` and connected to the local service running
on `http://localhost:8080`.

### Adding the MCP server to `Claude`

```shell
claude mcp add --transport http evalhub http://localhost:3001
```
