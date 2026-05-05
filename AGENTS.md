# AGENTS.md

This file provides guidance to AI agents when working with code in this repository.

This repository contains the following components:

- The EvalHub API Service
- The EvalHub MCP Service
- The EvalHub Runtime Sidecar Container
- The EvalHub Runtime Init Container

## Build and Development Commands

For a full list of Makefile targets, run `make help`.

### Running the API service

```bash
make start-service                # default port 8080
PORT=3000 make start-service      # custom port
make stop-service
go run cmd/eval_hub/main.go       # direct Go run
```

### Building

```bash
make build              # all binaries (service, init, sidecar, mcp) into bin/
make build-service      # API service only
make build-mcp          # evalhub-mcp only
./bin/eval-hub
./bin/evalhub-mcp
```

### Testing

```bash
make test               # unit tests (auth, internal, cmd, pkg, …)
make test-fvt           # godog FVT (tests/features)
make test-all           # unit + FVT + FVT against started server
make test-coverage      # HTML under bin/

go test -v ./internal/eval_hub/handlers -run TestHandleName
go test -v ./tests/features -run TestFeatureName
```

### Code Quality

```bash
make fmt                # Format code with go fmt
make lint               # Run go vet
make vet                # Run go vet (same as lint)
```

**Always run `make fmt lint` after file changes and before committing.** This ensures consistent formatting and catches issues early.

### Go Version

**Do not modify the Go version in `go.mod`.** The version specified there is the source of truth. If your local Go toolchain is older, use `GOTOOLCHAIN=auto` to let Go automatically download the required version. Never downgrade `go.mod` to match a locally installed toolchain.

### Dependencies

```bash
make install-deps       # Download and tidy dependencies (requires Python 3 for test color output via scripts/grcat)
make update-deps        # Update all dependencies to latest
# Note: uv (https://docs.astral.sh/uv/) is required for `make test-fvt` and `make start-service` (manages Python venv and test dependencies)
```

### Cleanup

```bash
make clean              # Remove build artifacts and coverage files
```

### Database (PostgreSQL for local/dev)

Targets are defined under `tests/postgres` (run from that directory).

```bash
cd tests/postgres
make install-postgres
make start-postgres
make stop-postgres
make create-database
make create-user
make grant-permissions
```

## Git commits

Use [Conventional Commits](https://www.conventionalcommits.org/) with an optional scope (e.g. `feat(http): …`).
Accepted type prefixes: `build`, `bump`, `chore`, `ci`, `docs`, `feat`, `fix`, `perf`, `refactor`, `revert`, `style`, `test`.

When a change is assisted by AI, add one of these lines, as appropriate, to the **end** of the commit message body (after the subject and any description), as Git trailers:

```text
Assisted-by: Cursor
Made-with: Cursor
Generated with: Claude Code
```

## Architecture Overview

Layout and request flow: **ARCHITECTURE.md** (in this repository). Supplementary docs: <https://github.com/eval-hub/eval-hub.github.io>.

### Project Structure

This project follows the standard Go project layout with a clear separation between public entry points (`cmd/`)
and private application code (`internal/`).
See **ARCHITECTURE.md** for a concise layout and request flow.

- **cmd/eval_hub/** - Main API service entry point
- **cmd/evalhub_mcp/** - MCP server entry point (stdio and HTTP transports)
- **cmd/eval_runtime_init/** - Init container for Kubernetes job pods
- **cmd/eval_runtime_sidecar/** - Sidecar for job pods (proxy, readiness, termination log)
- **pkg/api/** - Shared API types (IDs, errors, request/response shapes)
- **pkg/evalhubclient/** - HTTP client library for the eval-hub REST API (used by MCP server and external consumers)
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
- **internal/evalhub_mcp/config/** - MCP server configuration (CLI flags, YAML profiles, env vars)
- **internal/evalhub_mcp/server/** - MCP server setup (transport selection, capabilities, client wiring)
- **docs/src/openapi.yaml** - OpenAPI 3.1.0 specification source; bundled/public copies under **docs/** (see `make generate-public-docs`)
- **tests/features/** - BDD-style FVT tests using godog

### Server Lifecycle

Main function (`cmd/eval_hub/main.go`) implements graceful shutdown:

1. Creates logger and loads service, provider, and collection config (and optional auth config when enabled)
2. Wires storage, validator, runtime, and MLflow client
3. Creates server with `server.NewServer(logger, serviceConfig, authConfig, storage, validate, runtime, mlflowClient)`
4. Starts server in a goroutine
5. Waits for SIGINT/SIGTERM
6. Gracefully shuts down with a bounded timeout

### MCP Server

The MCP (Model Context Protocol) server exposes eval-hub functionality to AI agents. Entry point: `cmd/evalhub_mcp/main.go`.

```bash
# Run directly
go run cmd/evalhub_mcp/main.go                          # stdio transport (default)
go run cmd/evalhub_mcp/main.go --transport http          # HTTP/SSE on localhost:3001
go run cmd/evalhub_mcp/main.go --transport http --port 4000 --host 0.0.0.0

# Build and run
make build-mcp
./bin/evalhub-mcp --version
./bin/evalhub-mcp --transport http

go test -v ./cmd/evalhub_mcp/ ./internal/evalhub_mcp/...
```

**CLI flags:** `--transport stdio|http`, `--host`, `--port`, `--config`, `--insecure`, `--version`

**Configuration precedence:** CLI flags > YAML config (`~/.evalhub/config.yaml`) > env vars (`EVALHUB_BASE_URL`, `EVALHUB_TOKEN`, `EVALHUB_TENANT`, `EVALHUB_INSECURE`)

**Architecture:**

- `server.New()` creates the MCP server with advertised capabilities (tools, resources, prompts)
- `server.NewEvalHubClient()` creates an eval-hub API client from config
- `server.RegisterHandlers()` wires tool/resource/prompt handlers; handlers access the API client via closures
- Server metadata (name, version+build hash) is returned in the MCP `initialize` handshake
- Both transports share the same `*mcp.Server` instance, so capability listings are identical
- Uses `github.com/modelcontextprotocol/go-sdk` (Go MCP SDK)

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
