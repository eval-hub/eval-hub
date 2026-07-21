#!/usr/bin/env bash
# Run a command while holding an exclusive lock on tests/mlflow/bin/.mlflow.lock.
# Uses Python fcntl so it works on macOS (no flock(1)) and Linux.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MLFLOW_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
LOCK_FILE="${MLFLOW_LOCK_FILE:-${MLFLOW_DIR}/bin/.mlflow.lock}"

if [ "$#" -lt 1 ]; then
    echo "usage: $0 <command> [args...]" >&2
    exit 2
fi

mkdir -p "$(dirname "${LOCK_FILE}")"

# Prefer python3; fall back to venv python when present.
PYTHON_BIN="python3"
if [ -x "${MLFLOW_DIR}/${VENV_DIR:-.venv}/bin/python" ]; then
    PYTHON_BIN="${MLFLOW_DIR}/${VENV_DIR:-.venv}/bin/python"
fi

exec "${PYTHON_BIN}" - "${LOCK_FILE}" "$@" <<'PY'
import fcntl
import os
import subprocess
import sys

lock_path = sys.argv[1]
cmd = sys.argv[2:]
if not cmd:
    sys.exit(2)

os.makedirs(os.path.dirname(lock_path) or ".", exist_ok=True)
with open(lock_path, "a+", encoding="utf-8") as lock_file:
    fcntl.flock(lock_file.fileno(), fcntl.LOCK_EX)
    raise SystemExit(subprocess.call(cmd))
PY
