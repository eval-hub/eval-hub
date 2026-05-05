---
paths:
  - "cmd/eval_hub/**"
  - "internal/eval_hub/**"
  - "config/auth.yaml"
  - "config/config.yaml"
  - "config/collections/**"
  - "config/providers/**"
---

# EvalHub API Service

## Build & Test Commands

- Build: `make build-service`
- Test all: `make clean test-all`
- Test single unit test: `go test -v ./internal/eval_hub -run TestHandleName`
- Test single integration test: `go test -v ./tests/features -run TestFeatureName`
- Build coverage: `make build-coverage`
- Test all coverage: `make test-coverage`
- Lint: `make lint`
- Formatting: `make fmt` and `make vet`

## Key Conventions

Follow existing handler and storage patterns; run `make fmt lint` before committing.

## Architecture

### ExecutionContext Pattern

Evaluation-related handlers take `*executioncontext.ExecutionContext` plus HTTP wrappers instead of raw `*http.Request` / `http.ResponseWriter`:

```go
func (h *Handlers) HandleCreateEvaluation(ctx *executioncontext.ExecutionContext, req http_wrappers.RequestWrapper, w http_wrappers.ResponseWrapper)
```

Service configuration, storage, and runtime live on **`handlers.Handlers`** (constructed in `server.setupRoutes`), not on `ExecutionContext`.

The `ExecutionContext`:

- Carries `context.Context` (from the request, so OTEL spans propagate)
- Holds request ID, request-scoped `*slog.Logger`, and `api.User` / `api.Tenant` (from `X-User` / `X-Tenant` when present)
- Is created per route via **`Server.newExecutionContext`**, which calls `executioncontext.NewExecutionContext` with the enhanced logger from **`Server.loggerWithRequest`**

This pattern enables:

- Automatic request ID tracking (from `X-Global-Transaction-Id` header or auto-generated UUID)
- Structured logging with consistent request metadata
- Type-safe user/tenant and logger threading without passing raw `http.ResponseWriter` into business logic

### Two-Tier Configuration System

Configuration uses Viper with a sophisticated loading strategy:

1. **config.yaml** (config/config.yaml) - Configuration file

Configuration supports:

- **Environment variable mapping**: Define in `env_mappings` (e.g., `PORT` → `service.port`)
- **Secrets from files**: Define in `secrets.mappings` with `secrets.dir` (secret file basename under that directory → config path, e.g. file `/tmp/db_password` → `database.password`)
- Values cascade from config.yaml to env vars to secrets

Example (matches `config/config.yaml` shape; keys under `env_mappings` are environment variable names, values are Viper config paths):

```yaml
env_mappings:
  PORT: service.port
secrets:
  dir: /tmp
  mappings:
    db_password: database.password
```

### Structured Logging with Request Enhancement

Uses zap (wrapped in slog interface) for high-performance structured JSON logging.

Loggers are enhanced per-request with:

- **request_id**: From `X-Global-Transaction-Id` header or auto-generated UUID
- **method**: HTTP method (GET, POST, etc.)
- **uri**: Request path
- **user_agent**: Client user agent
- **remote_addr**: Client IP address
- **remote_user**: Authenticated user (from URL or Remote-User header)
- **referer**: HTTP referer header

Enhancement happens in **`Server.loggerWithRequest`**, invoked from **`Server.newExecutionContext`**.

### Routing Pattern

Uses standard library `net/http.ServeMux` without a web framework:

- Basic handlers (health, status, OpenAPI) still use `http.ResponseWriter, *http.Request` at the route closure boundary
- Evaluation-related handlers receive `*executioncontext.ExecutionContext`, `http_wrappers.RequestWrapper`, and `http_wrappers.ResponseWrapper`
- Routes manually switch on HTTP method in handler functions
- `ExecutionContext` and wrappers are created at the route level before calling the handler

Example (matches `setupEvaluationJobsRoutes`):

```go
s.handleFunc(router, "/api/v1/evaluations/jobs", func(w http.ResponseWriter, r *http.Request) {
    ctx := s.newExecutionContext(r)
    resp := NewRespWrapper(w, ctx)
    req := s.newRequestWrapper(w, r)
    switch r.Method {
    case http.MethodPost:
        h.HandleCreateEvaluation(ctx, req, resp)
    case http.MethodGet:
        h.HandleListEvaluations(ctx, req, resp)
    }
})
```

### Metrics Collection

- Prometheus metrics exposed at `/metrics`
- Custom middleware in `internal/eval_hub/metrics` wraps all routes
- Metrics middleware records request duration and status codes

### Database Setup

#### sqlite

By default tests run with sqlite in-memory database, see the `database` section in `config/config.yaml`.

#### postgresql

Directory: `tests/postgres` (run these targets from that directory, e.g. `cd tests/postgres`).

```bash
make install-postgres   # Install PostgreSQL (macOS/Linux)
make start-postgres     # Start PostgreSQL service
make stop-postgres      # Stop PostgreSQL service
make create-database    # Create eval_hub database
make create-user        # Create eval_hub user
make grant-permissions  # Grant permissions to user
```

## Testing Strategy

### Unit Tests

Located alongside code in `*_test.go` files:

- Test individual handlers, middleware, server setup
- Use standard library `testing` package
- Found in: `auth/**/*_test.go`, `internal/**/*_test.go`, `cmd/**/*_test.go`, `pkg/**/*_test.go`
- Add `t.Parallel()` to new tests where safe — avoid it when the test mutates process-wide state (e.g. `t.Setenv`, `os.Stdout`, package-level globals)

#### FVT (Functional Verification Tests)

BDD-style tests using godog in `tests/features/`:

- Feature files describe scenarios in Gherkin syntax (`.feature` files)
- Step definitions in `step_definitions_test.go` implement steps
- Tests run against actual HTTP server
- Suite setup in `suite_test.go`

#### FVT Tags

- `@cluster` — tests that require a Kubernetes cluster
- `@local` — tests that only run locally (excluded from CI)
- `@mlflow` — tests requiring MLflow integration
- `@negative` — negative/error-path tests
- `@gha-wheel-sanity` — subset run during GHA wheel validation (`scripts/gha_wheel_sanity_test.sh`); the script starts the wheel-installed binary, waits for health, then runs `make test-fvt` with this tag
