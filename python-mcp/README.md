# eval-hub-mcp

This package distributes the compiled Go eval-hub-mcp server binary for multiple
platforms. It installs the binary directly into your `bin/` directory (`Scripts/` on Windows) with no
Python wrapper — no argument rewriting, no subprocess overhead, no Python
runtime required at execution time.

## Installation

```bash
pip install eval-hub-mcp
```

## Capabilities

With `EVALHUB_BASE_URL`, `EVALHUB_TOKEN`, and `EVALHUB_TENANT` configured, the server exposes MCP **tools** (`discover_providers`, `submit_evaluation`, `get_job_status`, `cancel_job`), **resources** (`evalhub://providers`, `evalhub://benchmarks`, `evalhub://collections`, `evalhub://jobs`, and more), and **prompts** (`edd_workflow`, `evaluate_model`, `compare_runs`). See [MCP.md](../MCP.md) in the eval-hub repository for the full reference.

## Usage

```bash
# Run in stdio mode (default, for IDE integration)
evalhub-mcp

# Run in HTTP mode
evalhub-mcp --transport http --port 3001

# Show version
evalhub-mcp --version
```

## Supported Platforms

- Linux: x86_64, arm64
- macOS: x86_64 (Intel), arm64 (Apple Silicon)
- Windows: x86_64

## For eval-hub-sdk Users

> **Note:** `eval-hub-sdk[mcp]` currently installs a Python FastMCP-based server. This will be replaced with `eval-hub-mcp` (the Go binary distributed by this package) in a future release.

If you're using [`eval-hub-sdk`](https://github.com/eval-hub/eval-hub-sdk), you can install the MCP server as an extra:

```bash
pip install eval-hub-sdk[mcp]
```

For more information, see the [eval-hub-sdk repository](https://github.com/eval-hub/eval-hub-sdk).

## License

Apache-2.0
