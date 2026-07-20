#!/bin/bash

# Install MLflow into the local uv virtualenv (see make init-python).

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MLFLOW_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
cd "${MLFLOW_DIR}"

REQUESTED_VERSION="${MLFLOW_VERSION:-3.8.1}"
REQUESTED_PYTHON_MAJOR_VERSION="${2:-3}"
REQUESTED_PYTHON_MINOR_VERSION="${3:-10}"
VENV_DIR="${VENV_DIR:-.venv}"

if [ ! -x "${VENV_DIR}/bin/python" ]; then
    echo "❌ Error: ${VENV_DIR}/bin/python not found."
    echo "   Run 'make init-python' in ${MLFLOW_DIR} first."
    exit 1
fi

PYTHON="${VENV_DIR}/bin/python"

PYTHON_VERSION=$("${PYTHON}" --version 2>&1 | awk '{print $2}')
echo "📦 Python version: ${PYTHON_VERSION} (${VENV_DIR})"

check_python_version() {
    local version=$1
    local major minor patch

    IFS='.' read -r major minor patch <<< "$version"
    patch=$(echo "$patch" | sed 's/[^0-9].*//')

    if [ "$major" -lt "${REQUESTED_PYTHON_MAJOR_VERSION}" ]; then
        return 1
    fi

    if [ "$major" -eq "${REQUESTED_PYTHON_MAJOR_VERSION}" ] && [ "$minor" -lt "${REQUESTED_PYTHON_MINOR_VERSION}" ]; then
        return 1
    fi

    return 0
}

if ! check_python_version "$PYTHON_VERSION"; then
    echo "❌ Error: Python ${REQUESTED_PYTHON_MAJOR_VERSION}.${REQUESTED_PYTHON_MINOR_VERSION} or higher is required."
    echo "   Current version: ${PYTHON_VERSION}"
    echo "   Recreate the venv: rm -rf ${VENV_DIR} && make init-python PYTHON=3.11"
    exit 1
fi

echo "✅ Python version check passed (>= ${REQUESTED_PYTHON_MAJOR_VERSION}.${REQUESTED_PYTHON_MINOR_VERSION})"

echo "📥 Installing MLflow..."
if ! command -v uv >/dev/null 2>&1; then
    echo "❌ Error: uv is not installed. Install from https://docs.astral.sh/uv/"
    exit 1
fi

if [[ -n "${REQUESTED_VERSION}" ]]; then
    uv pip install --python "${PYTHON}" "mlflow==${REQUESTED_VERSION}"
else
    uv pip install --python "${PYTHON}" mlflow
fi

if [ -x "${VENV_DIR}/bin/mlflow" ]; then
    MLFLOW_INSTALLED_VERSION=$("${VENV_DIR}/bin/mlflow" --version 2>/dev/null | head -n 1)
    echo "✅ MLflow installed successfully!"
    echo "   Version: ${MLFLOW_INSTALLED_VERSION}"
    if [[ -n "${REQUESTED_VERSION}" ]] && [[ "${MLFLOW_INSTALLED_VERSION}" != "${REQUESTED_VERSION}" ]]; then
        echo "❌ Error: installed MLflow version does not match requested ${REQUESTED_VERSION}"
        echo "   Got: ${MLFLOW_INSTALLED_VERSION}"
        echo "   Another install may have raced on ${VENV_DIR}; retry with the lock held."
        exit 1
    fi
    echo ""
    echo "🎉 MLflow is ready to use!"
    echo "   Run 'make start-mlflow' to start the server"
else
    echo "❌ Error: MLflow installation failed or mlflow not found in ${VENV_DIR}/bin"
    exit 1
fi
