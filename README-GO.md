# eval-hub

A Go API service built with net/http.

## Getting Started

### Prerequisites

- Go 1.25 or higher
- Podman (for container builds)

### Running the Service

#### Using Make (Recommended)

1. Install dependencies:
```bash
make install-deps
```

2. Run the server:
```bash
make start-service
```

The server will start on port 8080 by default. You can change this by setting the `PORT` environment variable:

```bash
PORT=3000 make start-service
```

To check the logs:
```bash
tail -f bin/service.log
```

3. Stop the server:
```bash
make stop-service
```

#### Using Go directly

1. Install dependencies:
```bash
go mod download
```

2. Run the server:
```bash
go run cmd/eval_hub/main.go
```

The server will start on port 8080 by default. You can change this by setting the `PORT` environment variable:

```bash
PORT=3000 go run cmd/eval_hub/main.go
```

### API Endpoints

#### Evaluations
- `POST /api/v1/evaluations/jobs` - Create Evaluation
- `GET /api/v1/evaluations/jobs` - List Evaluations
- `GET /api/v1/evaluations/jobs/{id}` - Get Evaluation Status
- `DELETE /api/v1/evaluations/jobs/{id}` - Cancel Evaluation
- `GET /api/v1/evaluations/jobs/{id}/summary` - Get Evaluation Summary

#### Benchmarks
- `GET /api/v1/evaluations/benchmarks` - List All Benchmarks

#### Collections
- `GET /api/v1/evaluations/collections` - List Collections
- `POST /api/v1/evaluations/collections` - Create Collection
- `GET /api/v1/evaluations/collections/{collection_id}` - Get Collection
- `PUT /api/v1/evaluations/collections/{collection_id}` - Update Collection
- `PATCH /api/v1/evaluations/collections/{collection_id}` - Patch Collection
- `DELETE /api/v1/evaluations/collections/{collection_id}` - Delete Collection

#### Providers
- `GET /api/v1/evaluations/providers` - List Providers
- `GET /api/v1/evaluations/providers/{provider_id}` - Get Provider

#### Health
- `GET /api/v1/health` - Health check endpoint

#### Status
- `GET /api/v1/status` - Service status endpoint

#### Metrics
- `GET /api/v1/metrics/system` - Get System Metrics
- `GET /metrics` - Prometheus metrics endpoint

#### Documentation
- `GET /openapi.yaml` - OpenAPI 3.1.0 specification
- `GET /docs` - Interactive API documentation (Swagger UI)

### Building

#### Using Make

Build the binary:
```bash
make build
```

Run the binary:
```bash
./bin/eval-hub
```

#### Using Go directly

Build the binary:
```bash
go build -o bin/eval-hub ./cmd/eval_hub
```

Run the binary:
```bash
./bin/eval-hub
```

### Container Build and Run

Build the container image:
```bash
podman build -t eval-hub:latest \
  --build-arg BUILD_NUMBER=0.0.1 \
  --build-arg BUILD_DATE=$(date -u +'%Y-%m-%dT%H:%M:%SZ') \
  -f Containerfile .
```

This builds the image with:
- Go 1.25 toolchain (UBI9 base)
- Build metadata (version 0.0.1 and timestamp)
- Multi-stage build for minimal final image

Run the container locally:
```bash
podman run --rm -p 8080:8080 eval-hub:latest
```

The container will be available at `http://localhost:8080`.

### Makefile Targets

The project includes a Makefile with common development tasks:

- `make help` - Display all available targets
- `make clean` - Remove build artifacts
- `make build` - Build the binary
- `make start-service` - Start the application
- `make stop-service` - Stop the application
- `make lint` - Lint the code (runs go vet)
- `make fmt` - Format code with go fmt
- `make vet` - Run go vet
- `make test` - Run unit tests
- `make test-fvt` - Run FVT (Functional Verification Tests) using godog
- `make test-all` - Run all tests (unit + FVT)
- `make test-coverage` - Run unit tests with coverage report
- `make install-deps` - Install and tidy dependencies

## Project Structure

This project follows the [standard Go project layout](https://github.com/golang-standards/project-layout):

```
eval-hub/
├── cmd/
│   └── eval_hub/          # Main application entry point
│       └── main.go
├── internal/               # Private application code
│   ├── constants/         # Shared constants
│   │   └── log_fields.go  # Log field name constants
│   ├── handlers/          # HTTP handlers
│   │   ├── handlers.go     # Basic handlers (health, status)
│   │   ├── evaluations.go  # Evaluation-related handlers
│   │   ├── openapi.go      # OpenAPI documentation handlers
│   │   ├── handlers_test.go
│   │   └── openapi_test.go
│   ├── metrics/           # Prometheus metrics
│   │   ├── metrics.go
│   │   ├── middleware.go
│   │   └── middleware_test.go
│   └── server/            # Server setup and configuration
│       ├── server.go       # Server implementation
│       ├── logger.go        # Logger creation and configuration
│       └── server_test.go
├── api/                 # API specifications
│   └── openapi.yaml     # OpenAPI 3.1.0 specification
├── tests/               # Test files
│   └── features/        # BDD feature files and step definitions
│       ├── health.feature
│       ├── status.feature
│       ├── metrics.feature
│       ├── suite_test.go
│       └── step_definitions_test.go
├── Makefile
├── go.mod
└── README.md
```

## Testing

The project includes comprehensive test coverage:

### Unit Tests

Unit tests are located alongside the code in `*_test.go` files:
- `internal/handlers/handlers_test.go` - Handler unit tests
- `internal/handlers/openapi_test.go` - OpenAPI handler tests
- `internal/metrics/middleware_test.go` - Metrics middleware tests
- `cmd/eval_hub/server/server_test.go` - Server unit tests

Run unit tests:
```bash
make test
```

### FVT (Functional Verification Tests)

FVT tests use [godog](https://github.com/cucumber/godog) for BDD-style testing:
- Feature files in `tests/features/*.feature`
- Step definitions in `tests/features/step_definitions_test.go`

Run FVT tests:
```bash
make test-fvt
```

Run all tests:
```bash
make test-all
```

## Features

### Structured Logging

The service uses [zap](https://github.com/uber-go/zap) for high-performance structured JSON logging. Each request automatically includes:

- **Request ID**: Extracted from `X-Global-Transaction-Id` header or auto-generated UUID
- **HTTP Method**: Request method (GET, POST, etc.)
- **URI**: Request path
- **User Agent**: Client user agent string
- **Remote Address**: Client IP address
- **Remote User**: Authenticated user (if available)
- **Referer**: HTTP referer header (if present)

All log entries are automatically enriched with these fields for better traceability and debugging.

### Execution Context

All evaluation-related handlers receive an `ExecutionContext` that includes:
- Logger with request-specific fields
- Evaluation configuration (timeouts, retries, etc.)
- Model and benchmark specifications
- Metadata and experiment information

### Dependencies

Key dependencies:
- **zap** (`go.uber.org/zap`) - Structured logging
- **Prometheus** (`github.com/prometheus/client_golang`) - Metrics collection
- **godog** (`github.com/cucumber/godog`) - BDD testing framework
- **uuid** (`github.com/google/uuid`) - UUID generation
