# Feature Tests

This directory contains Cucumber/Gherkin feature tests for the eval-hub using the [godog](https://github.com/cucumber/godog) framework.

## Test Execution Modes

The tests support two execution modes:

### Remote Server Mode

When the `SERVER_URL` environment variable is set, the tests will run against a remote server instead of starting a local instance.

```bash
export SERVER_URL="https://api.example.com"
go test ./tests/features/...
```

The `SERVER_URL` should be a fully qualified URL (e.g., `http://localhost:8080` or `https://api.example.com`).

### Optional Model Overrides

You can override the model fields in the test payloads using environment variables:

- `MODEL_URL` (defaults to `http://test.com`)
- `MODEL_NAME` (defaults to `test`)

Example:

```bash
export MODEL_URL="http://granite-llm-metrics.prabhu.svc.cluster.local:8080/v1"
export MODEL_NAME="granite-llm"
```

### Local Server Mode (Default)

If `SERVER_URL` is not set, the tests will automatically start the server in a separate goroutine before running the test suite. The server will be started on:

- Port `8080` by default, or
- The port specified by the `PORT` environment variable

```bash
# Use default port 8080
go test ./tests/features/...

# Use custom port
export PORT=9090
go test ./tests/features/...
```

When running in local server mode, the tests will:
1. Start the server in a background goroutine during test suite initialization
2. Wait for the server to be ready by checking the health endpoint
3. Automatically shut down the server after all tests complete

## Test Structure

- **Feature files** (`.feature`): Gherkin syntax test scenarios
- **Step definitions** (`step_definitions_test.go`): Implementation of test steps
- **Test suite** (`suite_test.go`): Test suite configuration and initialization

## Running Tests

### Using Make

The recommended way to run the feature tests is using the Make target:

```bash
make test-fvt
```

This runs the tests with verbose output enabled.

### Using Go Test Directly

Run all feature tests:

```bash
go test ./tests/features/...
```

Run with verbose output:

```bash
go test -v ./tests/features/...
```

Run a specific feature.
