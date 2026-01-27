# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build and Development Commands

### Running the Service
```bash
make run                # Run on default port 8080
PORT=3000 make run      # Run on custom port
go run cmd/eval_hub/main.go  # Direct Go execution
```

### Building
```bash
make build              # Build binary to bin/eval-hub
./bin/eval-hub  # Run the built binary
```

### Testing
```bash
make test               # Run unit tests (internal/...)
make test-fvt           # Run FVT tests using godog (tests/features/...)
make test-all           # Run all tests (unit + FVT)
make test-coverage      # Generate coverage report (coverage.html)

# Run specific unit test
go test -v ./internal/handlers -run TestHandleName

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
make install-deps       # Download and tidy dependencies
make update-deps        # Update all dependencies to latest
```

### Database Setup
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

## Architecture Overview

### Project Structure
This project follows the standard Go project layout with a clear separation between public entry points (`cmd/`) and private application code (`internal/`).

- **cmd/eval_hub/** - Main application entry point with configuration (server.yaml)
- **internal/config/** - Configuration loading with Viper
- **internal/constants/** - Shared constants (log field names, etc.)
- **internal/executioncontext/** - ExecutionContext pattern implementation
- **internal/handlers/** - HTTP request handlers
- **internal/logging/** - Logger creation and request enhancement
- **internal/metrics/** - Prometheus metrics and middleware
- **cmd/eval_hub/server/** - Server setup and routing
- **api/** - OpenAPI 3.1.0 specification
- **tests/features/** - BDD-style FVT tests using godog

### Key Architectural Patterns

#### ExecutionContext Pattern
All evaluation-related handlers receive an `ExecutionContext` instead of raw `http.Request`:

```go
func (h *Handlers) HandleCreateEvaluation(ctx *ExecutionContext, w http.ResponseWriter, r *http.Request)
```

The ExecutionContext:
- Contains a request-scoped logger with enriched fields
- Carries the service configuration
- Holds evaluation-specific state (model info, timeouts, retries, metadata)
- Is created in server route handlers via `executioncontext.NewExecutionContext(r, logger, config)`

This pattern enables:
- Automatic request ID tracking (from `X-Global-Transaction-Id` header or auto-generated UUID)
- Structured logging with consistent request metadata
- Type-safe passing of configuration and state

#### Two-Tier Configuration System
Configuration uses Viper with a sophisticated loading strategy:

1. **server.yaml** (config/server.yaml) - Default configuration loaded first
2. **config.yaml** (optional, searched in `.` and `..`) - Cluster-specific overrides

Configuration supports:
- **Environment variable mapping**: Define in `env.mappings` (e.g., `PORT` → `service.port`)
- **Secrets from files**: Define in `secrets.mappings` with `secrets.dir` (e.g., `/tmp/db_password` → `database.password`)
- Default values cascade from server.yaml to config.yaml to env vars to secrets

Example from server.yaml:
```yaml
env:
  mappings:
    service.port: PORT
secrets:
  dir: /tmp
  mappings:
    database.password: db_password
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

Enhancement happens in `logging.LoggerWithRequest(logger, r)`, called when creating ExecutionContext.

#### Routing Pattern
Uses standard library `net/http.ServeMux` without a web framework:
- Basic handlers (health, status, OpenAPI) receive `http.ResponseWriter, *http.Request`
- Evaluation-related handlers receive `*ExecutionContext, http.ResponseWriter, *http.Request`
- Routes manually switch on HTTP method in handler functions
- ExecutionContext created at route level before calling handler

Example:
```go
router.HandleFunc("/api/v1/evaluations/jobs", func(w http.ResponseWriter, r *http.Request) {
    ctx := executioncontext.NewExecutionContext(r, s.logger, s.serviceConfig)
    switch r.Method {
    case http.MethodPost:
        h.HandleCreateEvaluation(ctx, w, r)
    case http.MethodGet:
        h.HandleListEvaluations(ctx, w, r)
    }
})
```

#### Metrics Collection
- Prometheus metrics exposed at `/metrics`
- Custom middleware in `internal/metrics` wraps all routes
- Metrics middleware records request duration and status codes

### Testing Strategy

#### Unit Tests
Located alongside code in `*_test.go` files:
- Test individual handlers, middleware, server setup
- Use standard library `testing` package
- Found in: `internal/handlers/*_test.go`, `internal/metrics/*_test.go`, `cmd/eval_hub/server/*_test.go`

#### FVT (Functional Verification Tests)
BDD-style tests using godog in `tests/features/`:
- Feature files describe scenarios in Gherkin syntax (`.feature` files)
- Step definitions in `step_definitions_test.go` implement steps
- Tests run against actual HTTP server
- Suite setup in `suite_test.go`

### Server Lifecycle
Main function (cmd/eval_hub/main.go) implements graceful shutdown:
1. Creates logger and loads config
2. Creates server with `server.NewServer(logger, config)`
3. Starts server in goroutine
4. Waits for SIGINT/SIGTERM signal
5. Gracefully shuts down with 30-second timeout

### Important Implementation Notes

#### Configuration Discovery
When running locally:
- Loads `config/server.yaml` as defaults
- Searches for optional `config.yaml` in current directory and parent (errors ignored)
- Environment variables override file config
- Secrets from files (if directory exists) override everything

#### Request ID Tracking
All requests are tagged with a request ID for distributed tracing:
- Extracted from `X-Global-Transaction-Id` header if present
- Auto-generated UUID if header missing
- Automatically added to all log entries for that request
- Useful for correlating logs across services
