# eval-hub-server

This package is a thin Python wrapper that packages and distributes the compiled Go eval-hub server binary for multiple platforms. It handles platform detection and binary resolution so consumers can simply install and run.

It is primarily intended to be used as a dependency of `eval-hub-sdk`.

## Installation

```bash
pip install eval-hub-server
```

## Usage

### CLI

```bash
# Run with default settings (port 8080)
eval-hub-server

# Run in local mode
eval-hub-server --local

# Run with custom port 5000
PORT=5000 eval-hub-server --local
```

### Python module

```bash
python -m evalhub_server.main --local
```

### Programmatically

Requires the package to be installed. `get_binary_path()` raises `FileNotFoundError` or `RuntimeError` if the binary for your platform is not available.

```python
from evalhub_server import get_binary_path

# Get the path to the binary
binary_path = get_binary_path()

# Use it however you need (e.g., subprocess)
import subprocess
subprocess.run([binary_path, "--local"], check=True)
```

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

## Development

See [DEVELOPMENT.md](DEVELOPMENT.md) for build process details, local development setup, testing, and troubleshooting.

## License

Apache-2.0
