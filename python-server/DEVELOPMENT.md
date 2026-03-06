# eval-hub-server Development Guide

## Overview

The `eval-hub-server` Python package provides platform-specific binaries of the eval-hub server. It's designed to be installed as an optional extra for `eval-hub-sdk`.

## Package Structure

```
python-server/
├── pyproject.toml              # Package metadata and build config
├── setup.py                    # Post-install hook for executable permissions
├── README.md                   # User-facing documentation
├── DEVELOPMENT.md              # This file
├── .gitignore                  # Excludes build artifacts
└── evalhub_server/
    ├── __init__.py             # Main module with get_binary_path()
    └── binaries/               # Platform binaries (populated during CI)
        └── .gitkeep
```

## How It Works

### Build Process (GitHub Actions)

When a release is published in the eval-hub repository:

1. **Build Go Binaries** (`build-binaries` job)
   - Builds eval-hub for 5 platforms: Linux (x64, arm64), macOS (x64, arm64), Windows (x64)
   - Uses `CGO_ENABLED=0` for static binaries
   - Uploads each binary as an artifact

2. **Build Python Wheels** (`build-wheels` job)
   - Creates 5 platform-specific wheels
   - Downloads the appropriate binary for each platform
   - Packages the binary into the wheel
   - Renames wheels with correct platform tags (e.g., `manylinux_2_17_x86_64`)

3. **Publish to PyPI** (`publish` job)
   - Uses GitHub OIDC trusted publishing (no API tokens)
   - Publishes all 5 wheels to PyPI

### Runtime Behavior

When users install `eval-hub-server`, pip automatically selects the correct wheel for their platform. The package provides a single function:

```python
from evalhub_server import get_binary_path

# Returns path to the binary for current platform
binary_path = get_binary_path()
```

The function:
1. Detects the current platform (OS + architecture)
2. Locates the corresponding binary in the package
3. Returns the absolute path
4. Raises `FileNotFoundError` if the binary doesn't exist
5. Raises `RuntimeError` if the platform is unsupported

### Platform Detection

Supported platforms:
- **Linux**: `x86_64` (`eval-hub-linux-amd64`), `arm64/aarch64` (`eval-hub-linux-arm64`)
- **macOS**: `x86_64` (`eval-hub-darwin-amd64`), `arm64` (`eval-hub-darwin-arm64`)
- **Windows**: `amd64` (`eval-hub-windows-amd64.exe`)

## Using in eval-hub-sdk

The SDK includes the server as an optional extra:

```toml
[project.optional-dependencies]
server = [
    "eval-hub-server>=0.1.0a0",
]
```

Users install it with:

```bash
pip install eval-hub-sdk[server]
```

SDK code can check for availability:

```python
try:
    from evalhub_server import get_binary_path
    HAS_SERVER = True
except ImportError:
    HAS_SERVER = False

if HAS_SERVER:
    binary = get_binary_path()
    # Use the binary
```

See `eval-hub-sdk/examples/use_server.py` for a complete example.

## Local Development

### Building Locally

To test the package locally:

```bash
# Build a test binary (from eval-hub root)
cd /path/to/eval-hub
make build

# Copy to python-server package
mkdir -p python-server/evalhub_server/binaries
cp bin/eval-hub python-server/evalhub_server/binaries/eval-hub-$(uname -s | tr '[:upper:]' '[:lower:]')-$(uname -m)

# Build the wheel
cd python-server

# Option 1: Build with native platform tag (linux_x86_64, etc.)
uv build --wheel

# Option 2: Build with PyPI-compatible tag (same as CI)
WHEEL_PLATFORM=manylinux_2_17_x86_64 uv build --wheel  # Linux x64
WHEEL_PLATFORM=manylinux_2_17_aarch64 uv build --wheel  # Linux ARM64
WHEEL_PLATFORM=macosx_11_0_arm64 uv build --wheel      # macOS Apple Silicon
WHEEL_PLATFORM=macosx_10_9_x86_64 uv build --wheel     # macOS Intel
WHEEL_PLATFORM=win_amd64 uv build --wheel              # Windows

# Install locally
uv pip install dist/*.whl
```

### Testing Locally

```bash
# Install in development mode (includes dev dependencies: ruff, pytest)
cd python-server
uv sync --extra dev

# Test the import
uv run python -c "from evalhub_server import get_binary_path; print(get_binary_path())"

# Run unit tests
uv run pytest -v
```

### Linting

```bash
cd python-server

# Check lint errors and formatting
uv run ruff check . && uv run ruff format --check .

# Auto-fix lint errors and formatting
uv run ruff check --fix . && uv run ruff format .
```

## Version Synchronization

Keep versions synchronized between:
- `eval-hub/python-server/pyproject.toml` → `version = "0.1.0a0"`
- `eval-hub-sdk/pyproject.toml` → `server = ["eval-hub-server>=0.1.0a0"]`

Update both when releasing new versions.

## Troubleshooting

### "Binary not found" error

The user's platform may not be supported. Check that:
1. The platform is in the CI matrix
2. The binary was successfully built and packaged
3. The platform detection logic matches

### Wrong binary selected

Check the platform detection in `__init__.py:get_binary_path()`. Use:
```python
import platform
print(platform.system().lower())  # darwin, linux, windows
print(platform.machine().lower())  # x86_64, arm64, amd64, aarch64
```

### Wheel platform tags

**Without `WHEEL_PLATFORM`** (default local builds):
- `evalhub_server-0.1.0a0-py3-none-linux_x86_64.whl` (Linux)
- `evalhub_server-0.1.0a0-py3-none-macosx_10_9_x86_64.whl` (macOS Intel)
- `evalhub_server-0.1.0a0-py3-none-win_amd64.whl` (Windows)

**With `WHEEL_PLATFORM`** (CI builds or local with env var):
- `evalhub_server-0.1.0a0-py3-none-manylinux_2_17_x86_64.whl` (Linux x64)
- `evalhub_server-0.1.0a0-py3-none-manylinux_2_17_aarch64.whl` (Linux ARM64)
- `evalhub_server-0.1.0a0-py3-none-macosx_11_0_arm64.whl` (macOS Apple Silicon)
- `evalhub_server-0.1.0a0-py3-none-macosx_10_9_x86_64.whl` (macOS Intel)
- `evalhub_server-0.1.0a0-py3-none-win_amd64.whl` (Windows)

Both work for installation, but only `manylinux_*`/`macosx_*`/`win_*` wheels can be uploaded to PyPI (native `linux_*` tags are rejected).

If you see `py3-none-any.whl`, the wheel is platform-independent (wrong - means `root_is_pure` wasn't set to `False`).

## Security Considerations

- **Static binaries**: Use `CGO_ENABLED=0` to avoid glibc dependencies and security issues
- **Permissions**: `setup.py` makes binaries executable on Unix (chmod 755)
