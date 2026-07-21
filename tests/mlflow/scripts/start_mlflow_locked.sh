#!/usr/bin/env bash
# Install, stop-this-port, and start MLflow while the caller holds the shared lock.
# Invoked by make _start-mlflow-locked; expects MLFLOW_* / VENV_DIR in the environment.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MLFLOW_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
cd "${MLFLOW_DIR}"

MLFLOW_VERSION="${MLFLOW_VERSION:?MLFLOW_VERSION is required}"
VENV_DIR="${VENV_DIR:-.venv}"
MLFLOW_HOST="${MLFLOW_HOST:-127.0.0.1}"
MLFLOW_PORT="${MLFLOW_PORT:-5000}"
MLFLOW_BACKEND_STORE_URI="${MLFLOW_BACKEND_STORE_URI:-sqlite:///bin/mlflow_${MLFLOW_PORT}.db}"
MLFLOW_DEFAULT_ARTIFACT_ROOT="${MLFLOW_DEFAULT_ARTIFACT_ROOT:-./bin/mlruns_${MLFLOW_PORT}}"
MLFLOW_ENABLE_WORKSPACES="${MLFLOW_ENABLE_WORKSPACES:-false}"
MLFLOW_DISABLE_SECURITY_MIDDLEWARE="${MLFLOW_DISABLE_SECURITY_MIDDLEWARE:-false}"
MLFLOW_LOG_FILE="${MLFLOW_LOG_FILE:-bin/mlflow_${MLFLOW_PORT}.log}"
MLFLOW_TRACKING_URI="${MLFLOW_TRACKING_URI:-http://${MLFLOW_HOST}:${MLFLOW_PORT}}"

echo "📥 Installing MLflow server (version ${MLFLOW_VERSION})..."
MLFLOW_VERSION="${MLFLOW_VERSION}" VENV_DIR="${VENV_DIR}" ./scripts/download_mlflow.sh

echo "🛑 Stopping MLflow server on port ${MLFLOW_PORT}..."
MLFLOW_PORT="${MLFLOW_PORT}" ./scripts/stop_mlflow.sh || true
rm -f "bin/mlflow_${MLFLOW_PORT}.db" "tests/features/test_mlflow_${MLFLOW_PORT}.db"

echo "🚀 Starting MLflow server..."
echo "   Host: ${MLFLOW_HOST}"
echo "   Port: ${MLFLOW_PORT}"
echo "   Backend: ${MLFLOW_BACKEND_STORE_URI}"
echo "   Artifacts: ${MLFLOW_DEFAULT_ARTIFACT_ROOT}"
echo "   Log file: ${MLFLOW_LOG_FILE}"
echo ""

MLFLOW_HOST="${MLFLOW_HOST}" \
	MLFLOW_PORT="${MLFLOW_PORT}" \
	MLFLOW_BACKEND_STORE_URI="${MLFLOW_BACKEND_STORE_URI}" \
	MLFLOW_DEFAULT_ARTIFACT_ROOT="${MLFLOW_DEFAULT_ARTIFACT_ROOT}" \
	MLFLOW_ENABLE_WORKSPACES="${MLFLOW_ENABLE_WORKSPACES}" \
	MLFLOW_DISABLE_SECURITY_MIDDLEWARE="${MLFLOW_DISABLE_SECURITY_MIDDLEWARE}" \
	MLFLOW_LOG_FILE="${MLFLOW_LOG_FILE}" \
	VENV_DIR="${VENV_DIR}" \
	./scripts/run_mlflow.sh

echo "   MLflow server started in background. Use 'make stop-mlflow' to stop it."
echo ""
echo "   To use the server in your tests:"
echo "   export MLFLOW_TRACKING_URI=${MLFLOW_TRACKING_URI}"
