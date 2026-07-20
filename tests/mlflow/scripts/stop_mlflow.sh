#!/bin/bash

# Stop the MLflow server for MLFLOW_PORT only (default 5000).
# Avoids killing other port-specific servers used by parallel/sequential integration cases.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MLFLOW_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
cd "${MLFLOW_DIR}"
mkdir -p bin

PORT="${MLFLOW_PORT:-5000}"
PID_FILE="bin/mlflow_${PORT}.pid"

echo "🛑 Stopping MLflow server on port ${PORT}..."

kill_pid() {
    local pid="$1"
    if [ -z "${pid}" ]; then
        return 0
    fi
    if kill -0 "${pid}" 2>/dev/null; then
        kill "${pid}" 2>/dev/null || true
    fi
}

if [ -f "${PID_FILE}" ]; then
    PID="$(tr -d '[:space:]' < "${PID_FILE}" || true)"
    if [ -n "${PID}" ]; then
        kill_pid "${PID}"
        # MLflow/uvicorn may leave children listening after the parent exits.
        if command -v pkill >/dev/null 2>&1; then
            pkill -P "${PID}" 2>/dev/null || true
        fi
    fi
    rm -f "${PID_FILE}"
fi

# Free anything still listening on this port (parent or worker).
if command -v lsof >/dev/null 2>&1; then
    PIDS="$(lsof -tiTCP:"${PORT}" -sTCP:LISTEN 2>/dev/null || true)"
    if [ -n "${PIDS}" ]; then
        # shellcheck disable=SC2086
        kill ${PIDS} 2>/dev/null || true
    fi
fi

# Narrow fallback: match this port only (do not pkill all mlflow.server processes).
if command -v pkill >/dev/null 2>&1; then
    pkill -f "mlflow.*--port(=| )${PORT}([[:space:]]|$)" 2>/dev/null || true
fi

timeout=50
iterations=0
while [ "${iterations}" -lt "${timeout}" ]; do
    still_listening=0
    if command -v lsof >/dev/null 2>&1; then
        if lsof -tiTCP:"${PORT}" -sTCP:LISTEN >/dev/null 2>&1; then
            still_listening=1
        fi
    elif command -v pgrep >/dev/null 2>&1; then
        if pgrep -f "mlflow.*--port(=| )${PORT}([[:space:]]|$)" >/dev/null 2>&1; then
            still_listening=1
        fi
    fi
    if [ "${still_listening}" -eq 0 ]; then
        echo "🛑 MLflow server on port ${PORT} stopped"
        exit 0
    fi
    sleep 0.1
    iterations=$((iterations + 1))
done

# Last resort after graceful attempts timed out.
if command -v lsof >/dev/null 2>&1; then
    PIDS="$(lsof -tiTCP:"${PORT}" -sTCP:LISTEN 2>/dev/null || true)"
    if [ -n "${PIDS}" ]; then
        # shellcheck disable=SC2086
        kill -9 ${PIDS} 2>/dev/null || true
        sleep 0.2
        if ! lsof -tiTCP:"${PORT}" -sTCP:LISTEN >/dev/null 2>&1; then
            echo "🛑 MLflow server on port ${PORT} stopped (forced)"
            exit 0
        fi
    fi
fi

echo "❌ MLflow server on port ${PORT} is still running after 5 seconds"
exit 1
exit 1
