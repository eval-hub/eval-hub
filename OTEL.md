# OpenTelemetry instrumentation

This document describes what eval-hub instruments with OpenTelemetry (OTEL), how to enable it, and known gaps.

Implementation lives primarily in `internal/otel/` (SDK bootstrap) and is wired from `cmd/eval_hub` and `cmd/eval_runtime_sidecar`.

## Enabling OTEL

Configure the `otel` block in `config/config.yaml` (or environment overrides). Example:

```yaml
otel:
  enabled: true
  exporter_type: "otlp-grpc"      # otlp-grpc | otlp-http | stdout
  exporter_endpoint: "localhost:4317"
  exporter_insecure: false
  sampling_ratio: 1.0
  enable_tracing: true
  enable_metrics: true            # required for application metrics and OTEL-bridged /metrics
  # enable_logs: true
  # enable_job_container_logs: true # requires enable_logs; exports adapter logs at job completion
  # metric_export_interval: 60s
  # disable_database_otel_scan: false
  # service_name: "custom-service-name"
```

| Flag | Effect |
|------|--------|
| `otel.enabled` | Master switch; required for any OTEL export |
| `otel.enable_tracing` | TracerProvider and trace export |
| `otel.enable_metrics` | MeterProvider, application metrics, DB pool metrics, and OTEL-bridged Prometheus scrape |
| `otel.enable_logs` | LoggerProvider; bridges application `slog` output to OTEL Logs |
| `otel.enable_job_container_logs` | Fetches adapter container logs when a job reaches a terminal state and emits them as OTEL log records (requires `enable_logs`) |
| `prometheus.enabled` | Dedicated `/metrics` port; when combined with `otel.enable_metrics`, registers the OTEL Prometheus reader as a dual sink |

**Important:** Enabling `prometheus.enabled` alone does **not** expose HTTP, domain, or DB application metrics. Those require `otel.enabled` and `otel.enable_metrics`.

### Config helpers

- `IsOTELEnabled()` — `otel.enabled`
- `IsOTELMetricsEnabled()` — `otel.enabled` && `otel.enable_metrics`
- `IsOTELLogsEnabled()` — `otel.enabled` && `otel.enable_logs`
- `IsOTELJobContainerLogsEnabled()` — `IsOTELLogsEnabled()` && `otel.enable_job_container_logs`
- `IsOTELStorageScansEnabled()` — `otel.enabled` && !`disable_database_otel_scan`

### Startup order (eval-hub API)

1. `otel.SetupOTEL()` — sets global TracerProvider / MeterProvider / LoggerProvider
2. `metrics.Init()` — registers application HTTP and domain metric instruments (when metrics enabled)
3. `storage.NewStorage(...)` — opens `otelsql` driver and registers DB pool metrics when metrics enabled

Storage must be created **after** the MeterProvider is configured so `ReportDBStatsMetrics` registers correctly.

## Binaries

| Binary | OTEL SDK | Notes |
|--------|----------|-------|
| `eval-hub` (`cmd/eval_hub`) | Yes | Full traces, metrics, logs (optional); Prometheus dual-sink |
| `eval-runtime-sidecar` | Yes | When `otel` is present in `sidecar_config.json`; no Prometheus dual-sink |
| `evalhub-mcp` | No | — |
| `eval-runtime-init` | No | — |

Default OTEL resource `service.name` values:

- eval-hub API: `github.com/eval-hub/eval-hub`
- sidecar: `github.com/eval-hub/eval-runtime-sidecar` (set automatically when OTEL is propagated into job `sidecar_config.json`)

Override with `otel.service_name` in config.

## Export pipeline

`internal/otel/otel_sdk.go` (`SetupOTEL`) configures:

- **Traces** — OTLP gRPC, OTLP HTTP, or stdout; head sampling via `sampling_ratio`
- **Metrics** — OTLP gRPC, OTLP HTTP, or stdout; export interval via `metric_export_interval` (default 60s)
- **Logs** — OTLP gRPC, OTLP HTTP, or stdout (same `exporter_type` / endpoint as traces and metrics)
- **Propagators** — W3C Trace Context and Baggage
- **Resource** — process/OS/host/container attributes; optional ECS detection

When `prometheus.enabled` and `otel.enable_metrics` are both true, the OTEL Prometheus exporter is registered as an additional metric reader on the same `MeterProvider`. Cluster scraping uses the dedicated metrics server on port 8081; local mode also serves `/metrics` on the main API port.

---

## Traces

### eval-hub API — inbound HTTP

Every API route registered via `server.handle()` is wrapped with `otelhttp.NewHandler` when `otel.enabled` is true. Span names use the format `{METHOD} {route_pattern}`.

Routes **without** child handler spans (otelhttp parent span only):

- `/healthz`
- `/api/v1/health`
- OpenAPI and docs routes

Routes **with** additional child spans via `handlers.withSpan()`:

- Evaluations (`internal/eval_hub/handlers/evaluations.go`)
- Collections (`collections.go`)
- Providers (`providers.go`)

Child spans are created when `otel.enabled` is true (they use the global TracerProvider; export requires `enable_tracing`).

### eval-hub API — outbound HTTP

- **MLflow client** — `otelhttp.NewTransport` when `otel.enabled` (`internal/eval_hub/mlflow/mlflow.go`)

### Database traces

- **SQL storage** — `otelsql` wraps the driver when storage scans **or** metrics are enabled (`IsOTELStorageScansEnabled()` || `IsOTELMetricsEnabled()`)
- Produces client spans for queries when the tracer provider is configured
- Disable query spans with `disable_database_otel_scan: true` (metrics-only mode still uses `otelsql` when `enable_metrics` is true)

### eval-runtime-sidecar

When `otel.enabled` in `sidecar_config.json`:

- **Inbound** — `otelhttp` on `GET /health` and the proxy handler (`/`)
- **Outbound** — `otelhttp.NewTransport` on EvalHub, MLflow, model, and OCI HTTP clients

OTEL settings are written into job-pod `sidecar_config.json` from eval-hub server config when `otel.enabled` is true on the server (`internal/eval_hub/runtimes/k8s/sidecar_config_json.go`).

---

## Metrics

All application metrics below require `otel.enable_metrics` and a successful `metrics.Init()` after `SetupOTEL`.

### HTTP (eval-hub API)

| OTEL name | Prometheus scrape name (typical) | Source | Labels / notes |
|-----------|-----------------------------------|--------|----------------|
| `http.server.request.duration` | `http_server_request_duration_*` | `otelhttp` per route | Semconv HTTP attributes |
| `http.server.request.count` | `http_server_request_count` | `HTTPMetricsMiddleware` | `http.request.method`, `http.route`, `http.response.status_code`; unmatched routes → `http.route=not_found` |
| `http.server.active_requests` | `http_server_active_requests` | `HTTPMetricsMiddleware` | `http.request.method`, `url.scheme` |
| `http.server.request.body.size` | `http_server_request_body_size_*` | `otelhttp` per route | — |
| `http.server.response.body.size` | `http_server_response_body_size_*` | `otelhttp` per route | — |

The `/metrics` scrape endpoint itself is **not** wrapped with `otelhttp` or the semconv middleware (avoids self-scrape noise). The cluster metrics server on port 8081 is also uninstrumented.

### Evaluation jobs (domain)

| OTEL name | When recorded | Attributes |
|-----------|---------------|------------|
| `evalhub.evaluation_jobs` | Job created, cancelled, or runtime start failed | `action` = `created` \| `cancelled` \| `runtime_start_failed`; `runtime` on create/fail |
| `evalhub.evaluation_job_completions` | Job transitions into a terminal state | `state` = `completed` \| `failed` \| `cancelled` \| `partially_failed` |
| `evalhub.benchmark_runtime_errors` | K8s or local runtime fails to schedule/start a benchmark | `runtime` = `kubernetes` \| `local` |

Terminal-state completions are recorded from:

- Runtime callbacks (`runtimeStorage.UpdateEvaluationJob`)
- Events API (`POST /api/v1/evaluations/jobs/{id}/events`) via `recordEvaluationJobTerminalStateAfterUpdate()`
- Explicit cancel and synchronous runtime-start-failure paths in handlers

There is no end-to-end job duration histogram and no per-tenant label on domain metrics today.

### Database metrics

When `otelsql` is active and `enable_metrics` is set:

| OTEL name | Type | Source |
|-----------|------|--------|
| `go.sql.query_timing` | Histogram (ms) | `otelsql` per query |
| `go.sql.connections_max_open` | Observable gauge | `ReportDBStatsMetrics` |
| `go.sql.connections_open` | Observable gauge | pool stats |
| `go.sql.connections_in_use` | Observable gauge | pool stats |
| `go.sql.connections_idle` | Observable gauge | pool stats |
| `go.sql.connections_wait_count` | Observable counter | pool stats |
| `go.sql.connections_wait_duration` | Observable counter | pool stats |
| `go.sql.connections_closed_max_idle` | Observable counter | pool stats |
| `go.sql.connections_closed_max_idle_time` | Observable counter | pool stats |
| `go.sql.connections_closed_max_lifetime` | Observable counter | pool stats |

There are no semconv `db.client.*` metrics or explicit DB error counters beyond trace span status.

---

## Logs

When `otel.enable_logs` is true:

1. **Service logs** — `cmd/eval_hub` bridges the application `slog` logger to the global OTEL `LoggerProvider` (tee: existing stdout JSON logs are preserved). Implementation: `internal/otel/slog_bridge.go`.
2. **Export** — `newLoggerProvider` uses the configured `exporter_type` (`otlp-grpc`, `otlp-http`, or `stdout`) and attaches the same resource attributes as traces/metrics.

When `otel.enable_job_container_logs` is also true (eval-hub API only):

- On transition to a terminal job state (`completed`, `failed`, `partially_failed`, `cancelled`), eval-hub asynchronously calls `Runtime.GetEvaluationLogs` (tail capped at `DefaultLogTailLines`, 1000) and emits each log line as an OTEL log record with attributes such as `evalhub.job.id`, `evalhub.benchmark.id`, and `evalhub.log.source=container`.
- Hook points: `POST /api/v1/evaluations/jobs/{id}/events` and runtime-initiated status updates (`runtimeStorage.UpdateEvaluationJob`).
- Export runs in a background goroutine (30s timeout) so workload callbacks are not blocked.
- **Limitations:** cancelled jobs may delete runtime resources before logs are fetched; log export is not triggered on per-benchmark events (only overall job terminal transition).

Local validation: enable OTEL logs in `config/config.yaml`, run a job, and inspect logs in the SigNoz UI from `tests/otel/`.

---

## OpenShift / TrustyAI operator

On OpenShift, EvalHub is typically deployed via the [TrustyAI service operator](https://github.com/trustyai-explainability/trustyai-service-operator). Set `spec.otel` on the `EvalHub` custom resource; the operator renders matching keys under `otel:` in the instance ConfigMap.

| eval-hub `config.yaml` key | EvalHub CR `spec.otel` field | Notes |
|----------------------------|------------------------------|-------|
| `enabled` | *(presence of `spec.otel`)* | Omit `spec.otel` to disable OTEL |
| `exporter_type` | `exporterType` | `otlp-grpc`, `otlp-http`, or `stdout` |
| `exporter_endpoint` | `exporterEndpoint` | |
| `exporter_insecure` | `exporterInsecure` | |
| `sampling_ratio` | `samplingRatio` | String float, e.g. `"0.5"` |
| `enable_tracing` | `enableTracing` | |
| `enable_metrics` | `enableMetrics` | Required for HTTP metrics and OTEL-bridged `/metrics` |
| `enable_logs` | `enableLogs` | |
| `tracer_timeout` | `tracerTimeout` | Duration string, e.g. `"30s"` |
| `tracer_batch_interval` | `tracerBatchInterval` | Duration string, e.g. `"5s"` |
| `service_name` | `serviceName` | |
| `additional_attributes` | `additionalAttributes` | Map of strings |
| `enable_ecs_resource_detection` | `enableEcsResourceDetection` | |
| `disable_redirect_otel_logs` | `disableRedirectOtelLogs` | |
| `disable_database_otel_scan` | `disableDatabaseOtelScans` | |
| `metric_export_interval` | `metricExportInterval` | Duration string, e.g. `"60s"` |

`TLSConfig` is runtime-only and is not set from the CR. Job-pod sidecars inherit the OTEL block from the EvalHub service config when jobs are created.

---

## Known gaps and limitations

### Trace continuity

- **Runtime job execution** detaches from the HTTP request trace (`context.Background()` in `executeEvaluationJob`). Background K8s job creation, local subprocess work, and post-request status updates are not linked to the create-job HTTP span.
- **K8s API calls** (create/delete Job, ConfigMap, Secret) have no dedicated spans.
- **Runtime/init binaries** are not traced.

### Instrumentation coverage

| Area | Status |
|------|--------|
| `evalhub-mcp` | No OTEL |
| `eval-runtime-init` | No OTEL |
| K8s client (`client-go`) | No spans or metrics |
| Local subprocess lifecycle | No spans or metrics |
| MLflow operations | HTTP transport spans only (no business-level spans) |
| Health / OpenAPI / docs handlers | otelhttp span only; no `withSpan` children |
| Benchmark success / completion counters | Errors only (`evalhub.benchmark_runtime_errors`); no success counter |
| Job duration histogram | Not implemented |
| Metric exemplars (trace ↔ metric links) | Not implemented |

### Configuration and gating

- **`otel.enabled` vs signal flags** — `otelhttp` and `withSpan` gate on `otel.enabled`, not separately on `enable_tracing`. With tracing disabled, spans are created against the noop tracer (small overhead, no export).
- **`enable_logs`** — requires `exporter_type` (defaults to stdout when unset).
- **`enable_job_container_logs`** — no effect unless `enable_logs` is true; startup logs a warning if misconfigured.
- **Stdout trace exporter** — OTLP trace paths attach resource attributes; the stdout trace exporter path does not use the same resource setup as OTLP.
- **`service.version`** — not set on the OTEL resource (commented out in `createResource`).
- **Prometheus-only deployments** — without `otel.enable_metrics`, `/metrics` exposes default process/Go collectors only, not application HTTP or domain metrics.

### Sidecar

- OTEL in job pods depends on eval-hub having `otel.enabled` when the job ConfigMap is built.
- `TLSConfig` is not serialized into `sidecar_config.json`; job pods rely on `exporter_insecure` or file-based TLS settings in the JSON block.
- No sidecar-specific domain metrics (proxy errors, token cache, etc.).
- No Prometheus dual-sink on the sidecar process.

### Operations

- Manual integration against a real OTLP collector and live dual-sink `/metrics` verification remain recommended before production rollout.
- FVT `@metrics` scenarios auto-enable OTEL metrics when Prometheus scraping is configured but do not enable tracing.

### Local SigNoz (Podman)

For macOS or Linux local testing, run SigNoz (traces, metrics, and logs in one UI) with Podman Compose. Requires Podman and at least 4GB memory for the stack.

```bash
cd tests/otel
make start-signoz
# or: podman compose -f pours/deployment/compose.yaml up -d
```

Stop the stack:

```bash
cd tests/otel
make stop-signoz
# or: podman compose -f pours/deployment/compose.yaml down
```

| Endpoint | URL |
|----------|-----|
| SigNoz UI | <http://localhost:3301> |
| OTLP gRPC (send eval-hub traces/metrics here) | `localhost:4317` |
| OTLP HTTP | `localhost:4318` |

SigNoz UI is mapped to **3301** so it does not clash with eval-hub on `:8080`.

Point eval-hub at SigNoz (local plaintext gRPC):

```yaml
otel:
  enabled: true
  exporter_type: "otlp-grpc"
  exporter_endpoint: "localhost:4317"
  exporter_insecure: true
  enable_tracing: true
  enable_metrics: true
  metric_export_interval: 10s
```

Then start the API (`make start-service`) and generate traffic. In SigNoz, filter by `service.name` = `github.com/eval-hub/eval-hub` (or the sidecar service name). eval-hub dual-sink Prometheus metrics remain on `:8081/metrics`.

**Refresh compose files** from the upstream SigNoz Foundry example:

```bash
cd tests/otel
./scripts/bootstrap-pours.sh
```

**Regenerate with Foundry** (optional; install `foundryctl` from [SigNoz Foundry](https://github.com/SigNoz/foundry)):

```bash
cd tests/otel
make forge-signoz
```

Files: `tests/otel/pours/deployment/`, `tests/otel/casting.yaml`, `tests/otel/scripts/bootstrap-pours.sh`.

---

## Related files

| Path | Role |
|------|------|
| `internal/otel/otel_sdk.go` | SDK bootstrap (tracer, meter, logger providers) |
| `internal/otel/span.go` | Shared `WithSpan` helper |
| `internal/eval_hub/metrics/` | Application metric instruments (domain + HTTP semconv) |
| `internal/eval_hub/server/http_metrics_middleware.go` | HTTP request count and active-request middleware |
| `internal/eval_hub/server/server.go` | Per-route `otelhttp` registration |
| `internal/eval_hub/handlers/otel.go` | Handler-level span wrapper |
| `internal/eval_hub/handlers/evaluation_metrics.go` | Terminal-state metric helper |
| `internal/eval_hub/storage/sql/sql.go` | `otelsql` and `ReportDBStatsMetrics` |
| `internal/eval_runtime_sidecar/server/server.go` | Sidecar inbound `otelhttp` |
| `internal/eval_runtime_sidecar/proxy/http_client.go` | Sidecar outbound `otelhttp` transport |
| `internal/otel/oteltest/` | Mock OTLP collector for export tests |
| `config/config.yaml` | Example OTEL and Prometheus configuration |

## Tests

- Unit: `go test ./internal/otel/... ./internal/eval_hub/metrics/...`
- Export integration: `internal/otel/otel_export_test.go` (mock OTLP gRPC collector)
- FVT: `make test-fvt` with `@metrics` tag (requires OTEL metrics + Prometheus in embedded server setup)
