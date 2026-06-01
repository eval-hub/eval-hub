# MLflow local development and tests

This directory runs a **local MLflow tracking server** for development and for godog BDD tests under `features/`. It uses a **dedicated Python virtual environment** in `tests/mlflow/.venv`, managed with [uv](https://docs.astral.sh/uv/). That venv is separate from the repository-root `.venv` used by `make test-fvt` and the Python server.

## Prerequisites

- [uv](https://docs.astral.sh/uv/) on your `PATH`
- Go (for `make test-godog-server`)
- `curl` or `wget` (optional; used to wait for server readiness)

## Quick start

```bash
make install-mlflow   # pip install mlflow into .venv
make start-mlflow       # start server in the background
```

Point clients at the server:

```bash
export MLFLOW_TRACKING_URI=http://127.0.0.1:5000
```

Stop the server:

```bash
make stop-mlflow
```

## Make targets

Run `make help` for a short list.

| Target | Description |
|--------|-------------|
| `help` | List targets and descriptions |
| `init-python` | Create `$(VENV_DIR)` with `uv venv` (default `.venv`, Python `3.14`) |
| `install-mlflow` | Depends on `init-python`; installs MLflow with `uv pip` into the venv |
| `start-mlflow` | Stop any existing server, then start MLflow in the background |
| `stop-mlflow` | Stop processes matching `mlflow.server` |
| `test-godog-server` | Start server, run godog tests (`-tags=godog`), then stop server |
| `clean` | Remove `bin/` (SQLite DB, logs, artifact root) |
| `cls` | Clear the terminal |

Typical workflow: **`init-python` → `install-mlflow` → `start-mlflow`**.

## Configuration

Override defaults on the `make` command line or in the environment:

| Variable | Default | Purpose |
|----------|---------|---------|
| `VENV_DIR` | `.venv` | uv virtualenv location (under `tests/mlflow`) |
| `PYTHON` | `3.14` | Python version passed to `uv venv` |
| `MLFLOW_VERSION` | `3.13.0` | MLflow package version for `uv pip install` (see **MLflow version note** below) |
| `MLFLOW_HOST` | `127.0.0.1` | Server bind address |
| `MLFLOW_PORT` | `5000` | Server port |
| `MLFLOW_BACKEND_STORE_URI` | `sqlite:///bin/mlflow.db` | Tracking store URI |
| `MLFLOW_DEFAULT_ARTIFACT_ROOT` | `./bin/mlruns` | Artifact storage directory |
| `MLFLOW_TRACKING_URI` | `http://127.0.0.1:5000` | Used by `test-godog-server` and client apps |

Examples:

```bash
make init-python PYTHON=3.12
make install-mlflow MLFLOW_VERSION=3.8.1
make start-mlflow MLFLOW_PORT=5001
MLFLOW_TRACKING_URI=http://127.0.0.1:5001 make test-godog-server
```

## What gets created

- **`.venv/`** — Python environment with `mlflow` CLI (`tests/mlflow/.venv/bin/mlflow`)
- **`bin/mlflow.db`** — SQLite tracking backend (when using default `MLFLOW_BACKEND_STORE_URI`)
- **`bin/mlruns/`** — Local artifact store
- **`bin/mlflow.log`** — Server stdout/stderr from the last `start-mlflow`

`make clean` removes `bin/` only; it does not delete `.venv`. To recreate Python from scratch:

```bash
rm -rf .venv && make init-python install-mlflow
```

## Godog tests

MLflow-specific scenarios live in `features/` and are built with the `godog` tag. They expect **`MLFLOW_TRACKING_URI`** to point at a running server.

```bash
# Managed server (start → test → stop)
make test-godog-server

# Or use your own server
make start-mlflow
export MLFLOW_TRACKING_URI=http://127.0.0.1:5000
go test -v -tags=godog ./...
make stop-mlflow
```

Repository-wide FVT (`make test-fvt` from the repo root) **excludes** `@mlflow` scenarios by default; use this directory when working on MLflow integration tests.

## Scripts

| Script | Role |
|--------|------|
| `scripts/download_mlflow.sh` | Installs MLflow into `$(VENV_DIR)` via `uv pip install` |
| `scripts/run_mlflow.sh` | Starts `mlflow server`, waits for `/health`, logs to `bin/mlflow.log` |
| `scripts/stop_mlflow.sh` | Stops background MLflow server processes |

`make start-mlflow` installs MLflow first via its `install-mlflow` dependency. The `run_mlflow.sh` script itself does **not** auto-install; it exits with guidance if `mlflow` is missing. Run `make clean` if there are issues with the database.

## MLflow version note

Eval-hub probes `GET /api/3.0/mlflow/server-info` at startup and only sends the `X-MLFLOW-WORKSPACE` header when the server reports `workspaces_enabled: true`.

A default local server from `make start-mlflow` does **not** enable workspaces. If you install **MLflow 3.10+** (for example `MLFLOW_VERSION=3.13.0`) and eval-hub still sent workspace headers (for example from `X-Tenant` on job create), MLflow 3.13+ returned `FEATURE_DISABLED` and job creation failed. Eval-hub now skips the workspace header in that case.

For local dev with eval-hub + this Makefile server, stay on the latest version with `--enable-workspaces` unless testing other versions or features.

When workspaces are enabled, eval-hub **creates the workspace automatically** (via `POST /api/3.0/mlflow/workspaces`) before creating experiments, using the tenant from `X-Tenant` as the workspace name (for example `test-tenant`). You do not need to create workspaces manually in the MLflow UI for FVT-style flows.

## Relation to eval-hub

- Eval-hub service MLflow settings (e.g. `MLFLOW_TRACKING_URI` in config) are documented in the main [AGENTS.md](../../AGENTS.md) and config YAML.
- This tree is only for **local MLflow server + MLflow-focused godog tests**, not for building eval-hub binaries.
