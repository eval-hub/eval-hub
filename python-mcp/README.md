# evalhub-mcp

This package distributes the compiled Go evalhub-mcp server binary for multiple
platforms. It installs the binary directly into your `bin/` directory (`Scripts/` on Windows) with no
Python wrapper — no argument rewriting, no subprocess overhead, no Python
runtime required at execution time.

## Installation

```bash
pip install evalhub-mcp
```

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

If you're using [`eval-hub-sdk`](https://github.com/eval-hub/eval-hub-sdk), you can install the MCP server binary as an extra:

```bash
pip install eval-hub-sdk[mcp]
```

For more information, see the [eval-hub-sdk repository](https://github.com/eval-hub/eval-hub-sdk).

## License

Apache-2.0
