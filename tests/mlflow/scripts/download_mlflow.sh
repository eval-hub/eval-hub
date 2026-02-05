#!/bin/bash

# Script to download and install MLflow server
# This script installs MLflow using pip

set -e

# For now we default to uasing version 3.8.1, we would need to see
# how we want to handle versions and to what extent the API is
# stable across versions.
REQUESTED_VERSION=${1:-"3.8.1"}
# REQUESTED_VERSION=${1:-""}

REQUESTED_PYTHON_MAJOR_VERSION=${2:-"3"}
REQUESTED_PYTHON_MINOR_VERSION=${3:-"10"}

# Check if Python is installed
if ! command -v python3 &> /dev/null; then
    echo "❌ Error: Python 3 is not installed. Please install Python 3 first."
    exit 1
fi

# Check if pip is installed
if ! command -v pip3 &> /dev/null; then
    echo "❌ Error: pip3 is not installed. Please install pip3 first."
    exit 1
fi

# Get Python version
PYTHON_VERSION=$(python3 --version 2>&1 | awk '{print $2}')
echo "📦 Python version: $PYTHON_VERSION"

# Check Python version >= 3.10
check_python_version() {
    local version=$1
    local major minor patch
    
    # Extract major, minor, and patch versions
    IFS='.' read -r major minor patch <<< "$version"
    
    # Remove any non-numeric characters from patch (e.g., "3.10.0a1" -> "0")
    patch=$(echo "$patch" | sed 's/[^0-9].*//')
    
    # Compare versions
    if [ "$major" -lt ${REQUESTED_PYTHON_MAJOR_VERSION} ]; then
        return 1
    fi
    
    if [ "$major" -eq ${REQUESTED_PYTHON_MAJOR_VERSION} ] && [ "$minor" -lt ${REQUESTED_PYTHON_MINOR_VERSION} ]; then
        return 1
    fi
    
    return 0
}

if ! check_python_version "$PYTHON_VERSION"; then
    echo "❌ Error: Python ${REQUESTED_PYTHON_MAJOR_VERSION}.${REQUESTED_PYTHON_MINOR_VERSION} or higher is required."
    echo "   Current version: $PYTHON_VERSION"
    echo "   Please upgrade Python to version ${REQUESTED_PYTHON_MAJOR_VERSION}.${REQUESTED_PYTHON_MINOR_VERSION} or higher."
    exit 1
fi

echo "✅ Python version check passed (>= ${REQUESTED_PYTHON_MAJOR_VERSION}.${REQUESTED_PYTHON_MINOR_VERSION})"

# Install MLflow
echo "📥 Installing MLflow..."
if [[ "${REQUESTED_VERSION}" != "" ]]; then
    python3 -m pip install mlflow==${REQUESTED_VERSION}
else
    python3 -m pip install mlflow
fi

# Verify installation
if command -v mlflow &> /dev/null; then
    MLFLOW_VERSION=$(mlflow --version 2>/dev/null | head -n 1)
    echo "✅ MLflow installed successfully!"
    echo "   Version: $MLFLOW_VERSION"
    echo ""
    echo "🎉 MLflow is ready to use!"
    echo "   Run 'make run-mlflow' to start the server"
else
    echo "❌ Error: MLflow installation failed or not found in PATH"
    exit 1
fi
