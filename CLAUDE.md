# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build and Development Commands

### Running the Service

```bash
make start-service                # Start service on default port 8080
PORT=3000 make start-service      # Start service on custom port
make stop-service                 # Stop service
go run cmd/eval_hub/main.go       # Direct Go execution
```

### Building

```bash
make build              # Build service, eval_runtime_init, and eval_runtime_sidecar into bin/
./bin/eval-hub          # Run the API service binary
```

### Testing

```bash
make test               # Run unit tests (./auth/..., ./internal/..., ./cmd/...)
make test-fvt           # Run FVT tests using godog (tests/features/...)
make test-all           # Run unit tests, FVT, then FVT against a started server (test-fvt-server)
make test-coverage      # HTML reports: bin/coverage.html and bin/coverage-init.html

# Run specific unit test
go test -v ./internal/eval_hub/handlers -run TestHandleName

# Run specific FVT test
go test -v ./tests/features -run TestFeatureName
```

### Code Quality

```bash
make lint               # Run go vet
make vet                # Run go vet
make fmt                # Format code with go fmt
```

### Dependencies

```bash
make install-deps       # Download and tidy dependencies (requires Python 3 for test color output via scripts/grcat)
make update-deps        # Update all dependencies to latest
```

### Database Setup

Directory: `tests/postgres` (run these targets from that directory, e.g. `cd tests/postgres`).

```bash
make install-postgres   # Install PostgreSQL (macOS/Linux)
make start-postgres     # Start PostgreSQL service
make stop-postgres      # Stop PostgreSQL service
make create-database    # Create eval_hub database
make create-user        # Create eval_hub user
make grant-permissions  # Grant permissions to user
```

### Cleanup

```bash
make clean              # Remove build artifacts and coverage files
```

## Git commits

Use [Conventional Commits](https://www.conventionalcommits.org/): `feat:`, `fix:`, `chore:`, `docs:`, `refactor:`, etc., with an optional scope (e.g. `feat(http): …`).

When a change is assisted by Cursor, add these lines to the **end** of the commit message body (after the subject and any description), as Git trailers:

```
Assisted-by: Cursor
Made-with: Cursor
```

## Architecture Overview

### Project Structure

This project follows the standard Go project layout with a clear separation between public entry points (`cmd/`) and private application code (`internal/`). See **ARCHITECTURE.md** for a concise layout and request flow.

- **cmd/eval_hub/** - Main API service entry point
- **cmd/eval_runtime_init/** - Init container for Kubernetes job pods
- **cmd/eval_runtime_sidecar/** - Sidecar for job pods (proxy, readiness, termination log)
- **pkg/api/** - Shared API types (IDs, errors, request/response shapes)
- **auth/** - Authentication configuration and HTTP middleware helpers
- **internal/eval_hub/abstractions/** - `Storage`, `Runtime`, and related interfaces
- **internal/eval_hub/config/** - Configuration loading with Viper
- **internal/eval_hub/constants/** - Shared constants (log field names, etc.)
- **internal/eval_hub/executioncontext/** - Per-request execution context (`Ctx`, logger, `User`, `Tenant`, etc.)
- **internal/eval_hub/handlers/** - HTTP handlers (depend on `Handlers` for config, storage, runtime)
- **internal/eval_hub/http_wrappers/** - `RequestWrapper` / `ResponseWrapper` abstractions for handlers
- **internal/eval_hub/runtimes/** - Local and Kubernetes runtime implementations
- **internal/eval_hub/storage/** - Persistence implementations (e.g. SQL)
- **internal/logging/** - Logger creation (zap backend, `slog` API)
- **internal/eval_hub/metrics/** - Prometheus metrics and middleware
- **internal/eval_hub/server/** - Server setup, routing, auth wiring, `newExecutionContext`
- **docs/src/openapi.yaml** - OpenAPI 3.1.0 specification source; bundled/public copies under **docs/** (see `make generate-public-docs`)
- **tests/features/** - BDD-style FVT tests using godog

### Key Architectural Patterns

#### ExecutionContext Pattern

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

#### Two-Tier Configuration System

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

#### Structured Logging with Request Enhancement

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

#### Routing Pattern

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

#### Metrics Collection

- Prometheus metrics exposed at `/metrics`
- Custom middleware in `internal/eval_hub/metrics` wraps all routes
- Metrics middleware records request duration and status codes

### Testing Strategy

#### Unit Tests

Located alongside code in `*_test.go` files:

- Test individual handlers, middleware, server setup
- Use standard library `testing` package
- Found in: `auth/**/*_test.go`, `internal/**/*_test.go`, `cmd/**/*_test.go`

#### FVT (Functional Verification Tests)

BDD-style tests using godog in `tests/features/`:

- Feature files describe scenarios in Gherkin syntax (`.feature` files)
- Step definitions in `step_definitions_test.go` implement steps
- Tests run against actual HTTP server
- Suite setup in `suite_test.go`

### Server Lifecycle

Main function (`cmd/eval_hub/main.go`) implements graceful shutdown:

1. Creates logger and loads service, provider, and collection config (and optional auth config when enabled)
2. Wires storage, validator, runtime, and MLflow client
3. Creates server with `server.NewServer(logger, serviceConfig, authConfig, storage, validate, runtime, mlflowClient)`
4. Starts server in a goroutine
5. Waits for SIGINT/SIGTERM
6. Gracefully shuts down with a bounded timeout

### Important Implementation Notes

#### Configuration Discovery

When running locally:

- Loads `config/config.yaml`
- Also loads provider and collection YAML definitions from the config directory (`LoadProviderConfigs`, `LoadCollectionConfigs` in `cmd/eval_hub/main.go`)
- Environment variables override file config
- Secrets from files (if directory exists) override everything

#### Eval runtime sidecar (Kubernetes job pods)

- Loads **`sidecar_config.json`** only (default `/meta/sidecar_config.json`; local override via `--sidecarconfig`).
- **No `evalhub-config` ConfigMap** on job pods; proxy targets and TLS live in JSON (`eval_hub.base_url`, `mlflow.tracking_uri`, `mlflow.token_path`, CA paths, optional `eval_hub.token`).
- Ready and termination message paths are **fixed in the sidecar binary** (`/data/sidecar-ready`, `/data/termination-log`).
- Local dev: `config/sidecar_runtime_local.json` or `make start-sidecar`.

#### Request ID Tracking

All requests are tagged with a request ID for distributed tracing:

- Extracted from `X-Global-Transaction-Id` header if present
- Auto-generated UUID if header missing
- Automatically added to all log entries for that request
- Useful for correlating logs across services

## CVE fixing

### Instructions for CVE fixing

Find any CVEs in the repository dependencies and create a PR with the proposed fix in the repository.

Verify that there is not already an open `PR` that provides this fix, if an open `PR` already
exists then report the `PR` number and skip the rest.

#### Updating the golang version

Before updating to a new golang version check that this version is supported in the go-toolset that can be found here `registry.access.redhat.com/ubi9/go-toolset`. If the new golang version is not yet supported in `registry.access.redhat.com/ubi9/go-toolset` then move to the latest supported version, if possible, and report that the desired version is not yet supported by go-toolset.
The PR should also update the major golang version, if needed, in the Containerfile.

The go.mod must not be updated until the same version exists in go-toolset.

If there are other files in the repository that require updating due to new golang version then mention them in the PR.
Use `go-version-file: "go.mod"` in the github actions where possible.

#### npm devDependencies

If updating any dependencies related to `npm` then verify that the documentation
build still works by running `make documentation`.
If `make documentation` changes any files in the `docs` directory then add them to the `PR`.
