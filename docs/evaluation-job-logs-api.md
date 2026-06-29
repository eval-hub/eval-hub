# Evaluation job logs API — summary

## New endpoints

| Method | Path | Purpose |
|--------|------|---------|
| `GET` | `/api/v1/evaluations/jobs/{id}/logs` | Workload logs for all benchmarks in a job |
| `GET` | `/api/v1/evaluations/jobs/{id}/benchmarks/{benchmark_index}/logs` | Workload logs for one benchmark |

**Query parameters:** `container`, `tail_lines`, `timestamps`, `previous`, `since_seconds`

**Response:** JSON (`EvaluationJobLogs` with per-container `streams`), or `Accept: text/plain` for concatenated text.

**Runtime behavior:**

- **Kubernetes:** Lists pods by `job_id` (+ optional `benchmark_index`), fetches container stdout/stderr via the Kubernetes API.
- **Local:** Reads `jobrun.log` under `/tmp/evalhub-jobs/{job_id}/{benchmark_index}/...`.

**Not included:** This does not read `logs_path` on benchmark results (adapter artifact files inside the pod).

OpenAPI definitions live under `docs/src/paths/` and are bundled into `docs/openapi.yaml`.

## Rationale for a separate API

Status polling (`GET /api/v1/evaluations/jobs/{id}`) is optimized for **lifecycle and results**: state, phases, metrics, pass/fail. Workload logs are a different concern: large, volatile, container-specific, and often only needed on failure or for live debugging.

A dedicated logs API keeps job GET responses small and safe to poll frequently, while allowing on-demand log retrieval with its own limits (`tail_lines`, byte caps) and content types (JSON vs plain text).

This complements [RHOAIENG-60198](https://redhat.atlassian.net/browse/RHOAIENG-60198): automatic capture into **service logs** is for operator correlation; the REST API is for **programmatic/client access** (UI, CLI, MCP).

## Pros

- **Separation of concerns** — Status stays lightweight; logs don’t bloat every poll.
- **On-demand** — Fetch only when needed; no DB persistence or retention burden.
- **Per-benchmark scoping** — Matches one Kubernetes Job per benchmark via `benchmark_index`.
- **Familiar model** — Same idea as `kubectl logs` (tail, since, container filter).
- **Shared runtime layer** — Same `JobPodLogFetcher` can back API, service-log capture, and future MCP tools.
- **Flexible output** — JSON for UIs/agents; plain text for CLI.

## Cons

- **Not true streaming** — Snapshot/poll only; no `kubectl logs -f` yet (would need SSE/chunked follow).
- **Best-effort availability** — Pods may be gone after Job TTL; no guarantee logs remain.
- **Operational cost** — Each request hits the Kubernetes API; polling logs + status doubles load.
- **Multi-stream noise** — One benchmark can return init, adapter, and sidecar streams unless filtered.
- **Two log concepts** — `logs_path` (artifact file) vs container logs can confuse users without clear docs.
- **RBAC** — eval-hub needs `pods/log` `get` in tenant namespaces.

## Typical usage

1. **Progress** — Poll `GET /jobs/{id}` (or MCP `get_job_status`).
2. **Debug failure** — `GET .../benchmarks/{i}/logs?container=adapter&tail_lines=500`.
3. **Live tail (near real-time)** — Poll logs every few seconds with `since_seconds` ≈ poll interval (full streaming is a possible follow-up).

## Related work

- Jira: [RHOAIENG-60198](https://redhat.atlassian.net/browse/RHOAIENG-60198) — optional automatic pod log capture into eval-hub service logs on terminal job state.
