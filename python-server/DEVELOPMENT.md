# eval-hub-server Development Guide

## Overview

The `eval-hub-server` Python package provides platform-specific binaries of the eval-hub server. It's designed to be installed as an optional extra for `eval-hub-sdk`.

## Package Structure

```
python-server/
├── pyproject.toml              # Package metadata and build config
├── setup.py                    # Post-install hook for executable permissions
├── uv.lock                     # Locked dependencies
├── README.md                   # User-facing documentation
├── DEVELOPMENT.md              # This file
├── .gitignore                  # Excludes build artifacts
├── evalhub_server/
│   ├── __init__.py             # Main module with get_binary_path()
│   ├── main.py                 # CLI entry point
│   └── binaries/               # Platform binaries (populated during CI)
│       └── .gitkeep
└── tests/
    └── test_main.py            # Unit tests
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
3. **Publish to TestPyPI** (`publish-test-pypi` job) — on `main` pushes or manual dispatch
  - Builds wheels with `.devN` version suffix (e.g., `0.2.0.dev42`)
  - Uses GitHub OIDC trusted publishing against the `testpypi` environment
  - Publishes all 5 wheels to [TestPyPI](https://test.pypi.org/p/eval-hub-server)
4. **Publish to PyPI** (`publish` job) — on tag pushes only
  - Uses GitHub OIDC trusted publishing (no API tokens)
  - Publishes all 5 wheels to PyPI with the clean version from `VERSION`

### Runtime Behavior

When users install `eval-hub-server`, pip automatically selects the correct wheel for their platform. The package provides a single function:

```python
from evalhub_server import get_binary_path

# Returns path to the binary for current platform
binary_path = get_binary_path()
```

It detects the current OS and architecture, locates the corresponding binary, and returns its absolute path (raises `FileNotFoundError` or `RuntimeError` if unavailable).

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
    "eval-hub-server>={VERSION}",  # Replace {VERSION} with the actual version, e.g. "eval-hub-server>=0.2.0"
]
```

Users install it with:

```bash
pip install eval-hub-sdk[server]
```

## Local Development

### Building Locally

Follow these steps to build and test the package locally:

#### 1. Clone and setup

```bash
git clone <repository>
cd eval-hub
uv venv
source .venv/bin/activate  # On Windows: .venv\\Scripts\\activate
uv pip install -e "./python-server[dev]"
```

#### 2. Build Go binaries

This step can be skipped if Go server binaries are already built. See the main project [README-GO.md](../README-GO.md) for details.

Example for macOS arm64:

```bash
make cross-compile CROSS_GOOS=darwin CROSS_GOARCH=arm64
```

See Makefile `build-all-platforms` target for other options.

#### 3. Lint and unit tests

```bash
# Check lint errors and formatting
uv run ruff check python-server && uv run ruff format --check python-server

# Auto-fix lint errors and formatting
uv run ruff check --fix python-server && uv run ruff format python-server

# Run unit tests
uv run pytest python-server/tests -v
```

#### 4. Build Python wheel

When you run `make build-wheel`, the Go binary is copied from `bin/` into the package automatically (step 2 must be done first). If the wheel build fails for missing tools, run `make install-wheel-tools` once.

Example for macOS arm64:

```bash
make build-wheel WHEEL_PLATFORM=macosx_11_0_arm64 WHEEL_BINARY=eval-hub-darwin-arm64
```

Or build all platform wheels at once:

```bash
make build-all-wheels
```

The wheel file will be under `python-server/dist` directory.

#### 5. Install locally

```bash
uv pip install python-server/dist/*.whl
```

Available platform values WHEEL_PLATFORM / WHEEL_BINARY:

- `manylinux_2_17_x86_64` / `eval-hub-linux-amd64` (Linux x64)
- `manylinux_2_17_aarch64` / `eval-hub-linux-arm64` (Linux ARM64)
- `macosx_11_0_arm64` / `eval-hub-darwin-arm64` (macOS Apple Silicon)
- `macosx_10_9_x86_64` / `eval-hub-darwin-amd64` (macOS Intel)
- `win_amd64` / `eval-hub-windows-amd64.exe` (Windows)

#### 6. Verify

```bash
# Test the import
uv run python -c "from evalhub_server import get_binary_path; print(get_binary_path())"
```

## Version Synchronization

The package version is read dynamically from the repo-root `VERSION` file. At build time, `make build-wheel` copies `VERSION` into `python-server/` so that setuptools can access it (setuptools rejects `file:` references outside the project root). The copied file is git-ignored.

For non-tag CI builds, a dev suffix is appended automatically via the `DEV_SUFFIX` make parameter. This is necessary because PyPI and TestPyPI reject duplicate versions — without `.devN`, only the first push to `main` would succeed for a given version. The `DEV_SUFFIX` parameter is only passed in CI via the workflow; `make build-all-wheels` (used for local development) does not forward it, since dev-suffixed versions are not needed locally.

```bash
# Produces version like 0.2.0.dev42
make build-wheel WHEEL_PLATFORM=macosx_11_0_arm64 WHEEL_BINARY=eval-hub-darwin-arm64 DEV_SUFFIX=dev42
```


| Trigger        | Version example         | Target   |
| -------------- | ----------------------- | -------- |
| Push to `main` | `0.2.0.dev<run_number>` | TestPyPI |
| Tag push       | `0.2.0`                 | PyPI     |


Keep the SDK dependency range in sync:

- `eval-hub-sdk/pyproject.toml` → `server = ["eval-hub-server>={VERSION}"]`

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

- **With `WHEEL_PLATFORM`** (CI or local with env var): wheels get PyPI-compatible tags (`manylinux_2_17_*`, `macosx_*`, `win_*`). Required for PyPI upload.
- **Without `WHEEL_PLATFORM`**: wheels get native platform tags (e.g. `linux_x86_64`) which work locally but are rejected by PyPI.
- If you see `py3-none-any.whl`, the wheel is incorrectly platform-independent — `root_is_pure` wasn't set to `False`.

## Security Considerations

- **Static binaries**: Use `CGO_ENABLED=0` to avoid glibc dependencies and security issues
- **Permissions**: `setup.py` makes binaries executable on Unix (chmod 755)

