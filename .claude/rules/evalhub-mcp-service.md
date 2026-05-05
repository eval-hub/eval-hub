---
paths:
  - "cmd/evalhub_mcp/**"
  - "internal/evalhub_mcp/**"
  - "config/mcp_local.yaml"
---

# EvalHub MCP Service

## Build & Test Commands

- Build: `make build-mcp`
- Test all: `make clean test` (or `make test` if you do not need a clean tree first)
- Test single test (example): `go test -v ./internal/evalhub_mcp/... -run TestRegisterHandlersIncludesPrompts`
- Lint: `make lint`
- Formatting: `make fmt vet`

**CLI flags:** `--transport stdio|http`, `--host`, `--port`, `--config`, `--insecure`, `--version`

**Configuration precedence:** CLI flags > YAML config (`~/.evalhub/config.yaml`) > env vars (`EVALHUB_BASE_URL`, `EVALHUB_TOKEN`, `EVALHUB_TENANT`, `EVALHUB_INSECURE`)

## Testing Strategy

### Unit Tests

Located alongside code in `*_test.go` files:

- Use standard library `testing` package
- Found in: `internal/**/*_test.go`, `cmd/**/*_test.go`, `pkg/**/*_test.go`
- MCP server tests use `mcp.NewInMemoryTransports()` for in-process initialize handshake and capability verification
- Add `t.Parallel()` to new tests where safe — avoid it when the test mutates process-wide state (e.g. `t.Setenv`, `os.Stdout`, package-level globals)
