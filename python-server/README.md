# eval-hub-server

This package provides the eval-hub server binary for multiple platforms.

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

This package is automatically built and published when a new release is created in the eval-hub repository. The build process:

1. Compiles Go binaries for all supported platforms
2. Creates platform-specific Python wheels containing the appropriate binary
3. Publishes to PyPI using trusted publishing

### Local Development

1. Clone and setup
   ```bash
   git clone <repository>
   cd eval-hub
   uv venv
   source .venv/bin/activate  # On Windows: .venv\\Scripts\\activate
   uv pip install -e "./python-server[dev]"
   ```

2. Build Go binaries

   This step can be skipped if Go server binaries are already built. See the main project README for details.

   Example for macOS arm64:
   ```bash
   make cross-compile CROSS_GOOS=darwin CROSS_GOARCH=arm64
   ```
   See Makefile `build-all-platforms` target for other options.

3. Install wheel and setuptools
   Install uv wheel and setuptools with the target `install-wheel-tools`
   ```bash
   make install-wheel-tools
   ```

4. Copy the Go-Binary

   python wheel looks for compiled Go-Binaries is a different path from the compiled out path. Copy to the desired location.
   ```bash
   make download-binary
   ```

5. Build Python wheel

   Example for macOS arm64:
   ```bash
   make build-wheel WHEEL_PLATFORM=macosx_11_0_arm64 WHEEL_BINARY=eval-hub-darwin-arm64
   ```
   See Makefile `build-all-wheels` target for other options.

   The wheel file will be under `python-server/dist` directory.

For usage, see [Usage](#usage).

## License

Apache-2.0
