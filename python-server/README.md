# eval-hub-server

This package distributes the compiled Go eval-hub server binary for multiple
platforms. It installs the binary directly into your `bin/` directory (`Scripts/` on Windows) with no
Python wrapper — no argument rewriting, no subprocess overhead, no Python
runtime required at execution time.

It is primarily intended to be used as a dependency of `eval-hub-sdk`.

## Installation

```bash
pip install eval-hub-server
```

## Usage

```bash
# Run with default settings (port 8080)
eval-hub-server

# Run in local mode
eval-hub-server -local

# Run with custom port 5000
PORT=5000 eval-hub-server -local

# The eval-hub command is also available
eval-hub -local
```

Both `eval-hub-server` and `eval-hub` run the same Go binary.
`eval-hub-server` is a shell shim that execs `eval-hub`.

## Supported Platforms

- Linux: x86_64, arm64
- macOS: x86_64 (Intel), arm64 (Apple Silicon)
- Windows: x86_64

## For eval-hub-sdk Users

If you're using [`eval-hub-sdk`](https://github.com/eval-hub/eval-hub-sdk), you can install the server binary as an extra:

```bash
pip install eval-hub-sdk[server]
```

For more information, see the [eval-hub-sdk repository](https://github.com/eval-hub/eval-hub-sdk).

## License

Apache-2.0
