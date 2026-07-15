#!/bin/bash

# Script to run MLflow server locally
# Default configuration: runs on http://localhost:5000

set -euo pipefail

# Colors for output
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MLFLOW_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
cd "${MLFLOW_DIR}"

VENV_DIR="${VENV_DIR:-.venv}"
if [ -d "${VENV_DIR}/bin" ]; then
    export PATH="${MLFLOW_DIR}/${VENV_DIR}/bin:${PATH}"
fi

# for now we wipe out the mlflow db and mlruns directory
if ! rm -rf bin/mlflow*.db bin/mlruns; then
    echo -e "${YELLOW}❌ Failed to wipe out mlflow db and mlruns directory${NC}"
    exit 1
fi
echo -e "${GREEN}✅ Wiped out mlflow db and mlruns directory${NC}"

mkdir -p bin

# Default values
HOST=${MLFLOW_HOST:-"127.0.0.1"}
PORT=${MLFLOW_PORT:-"5000"}
BACKEND_URI=${MLFLOW_BACKEND_STORE_URI:-"sqlite:///bin/mlflow.db"}
DEFAULT_ARTIFACT_ROOT=${MLFLOW_DEFAULT_ARTIFACT_ROOT:-"./bin/mlruns"}
MLFLOW_LOG_FILE=${MLFLOW_LOG_FILE:-"bin/mlflow_${PORT}.log"}
ENABLE_WORKSPACES=""
if [[ "${MLFLOW_ENABLE_WORKSPACES:-false}" == "true" ]]; then
    ENABLE_WORKSPACES="--enable-workspaces"
fi

log_to_file() {
    printf '%s %s\n' "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" "$*" >> "${MLFLOW_LOG_FILE}"
}

show_log_tail() {
    local lines="${1:-40}"
    if [ ! -f "${MLFLOW_LOG_FILE}" ]; then
        echo -e "${YELLOW}⚠️  Log file not found: ${MLFLOW_LOG_FILE}${NC}"
        return
    fi
    echo -e "${YELLOW}--- Last ${lines} lines of ${MLFLOW_LOG_FILE} ---${NC}"
    tail -n "${lines}" "${MLFLOW_LOG_FILE}" || true
    echo -e "${YELLOW}--- End of log tail ---${NC}"
}

echo -e "${BLUE}🚀 Starting MLflow server...${NC}"
echo ""
echo -e "Configuration:"
echo -e "  ${YELLOW}Host:${NC} $HOST"
echo -e "  ${YELLOW}Port:${NC} $PORT"
echo -e "  ${YELLOW}Backend Store URI:${NC} $BACKEND_URI"
echo -e "  ${YELLOW}Default Artifact Root:${NC} $DEFAULT_ARTIFACT_ROOT"
echo ""

# Check if MLflow is installed in the venv (or on PATH)
if ! command -v mlflow &> /dev/null; then
    echo -e "${YELLOW}⚠️  MLflow not found. Run 'make install-mlflow' first.${NC}"
    if [ ! -x "${VENV_DIR}/bin/python" ]; then
        echo -e "${YELLOW}   Hint: make init-python && make install-mlflow${NC}"
    fi
    exit 1
fi

# Create artifact root directory if it doesn't exist
if [ ! -d "$DEFAULT_ARTIFACT_ROOT" ]; then
    echo -e "${YELLOW}📁 Creating artifact root directory: $DEFAULT_ARTIFACT_ROOT${NC}"
    mkdir -p "$DEFAULT_ARTIFACT_ROOT"
fi

# Create backend database directory if using SQLite
if [[ "$BACKEND_URI" == sqlite://* ]]; then
    DB_PATH=$(echo "$BACKEND_URI" | sed 's|sqlite:///||')
    DB_DIR=$(dirname "$DB_PATH")
    if [ "$DB_DIR" != "." ] && [ ! -d "$DB_DIR" ]; then
        echo -e "${YELLOW}📁 Creating database directory: $DB_DIR${NC}"
        mkdir -p "$DB_DIR"
    fi
fi

LOG_DIR=$(dirname "${MLFLOW_LOG_FILE}")
if [ "$LOG_DIR" != "." ] && [ ! -d "$LOG_DIR" ]; then
    echo -e "${YELLOW}📁 Creating log directory: $LOG_DIR${NC}"
    mkdir -p "$LOG_DIR"
fi

echo -e "${GREEN}✅ Starting MLflow server...${NC}"
MLFLOW_VERSION=$(mlflow --version 2>/dev/null | head -n 1)
echo -e "${BLUE}📍 MLflow version: ${MLFLOW_VERSION}${NC}"
echo -e "${BLUE}📍 Server will be available at: http://$HOST:$PORT${NC} with options: ${ENABLE_WORKSPACES}"
echo -e "${BLUE}📍 Server logs: ${MLFLOW_LOG_FILE}${NC}"
echo -e "${YELLOW}💡 Press Ctrl+C to stop the server${NC}"
echo ""

: > "${MLFLOW_LOG_FILE}"
log_to_file "Starting MLflow server"
log_to_file "Version: ${MLFLOW_VERSION}"
log_to_file "Host: ${HOST}"
log_to_file "Port: ${PORT}"
log_to_file "Backend store URI: ${BACKEND_URI}"
log_to_file "Default artifact root: ${DEFAULT_ARTIFACT_ROOT}"
log_to_file "Enable workspaces: ${MLFLOW_ENABLE_WORKSPACES:-false}"
log_to_file "Command: mlflow server --host ${HOST} --port ${PORT} ${ENABLE_WORKSPACES} --backend-store-uri ${BACKEND_URI} --default-artifact-root ${DEFAULT_ARTIFACT_ROOT}"

# Log to file only — do not write server output to stdout. Background processes that
# keep stdout open will cause "make start-mlflow" (and go test exec) to hang waiting
# for EOF on the pipe.
mlflow server \
    --host "$HOST" \
    --port "$PORT" \
    ${ENABLE_WORKSPACES} \
    --backend-store-uri "$BACKEND_URI" \
    --default-artifact-root "$DEFAULT_ARTIFACT_ROOT" \
    >> "${MLFLOW_LOG_FILE}" 2>&1 &

MLFLOW_PID=$!

# Function to check if server is ready
wait_for_server() {
    local max_attempts=40  # 20 seconds with 0.5 second intervals
    local attempt=0
    local server_url="http://$HOST:$PORT"

    RED='\033[0;31m'
    echo -e "${YELLOW}⏳ Waiting for server to be ready...${NC}"

    while [ $attempt -lt $max_attempts ]; do
        # Check if process is still running
        if ! kill -0 "$MLFLOW_PID" 2>/dev/null; then
            echo -e "${RED}❌ MLflow server process died unexpectedly${NC}"
            show_log_tail
            return 1
        fi

        # Try to connect to the health endpoint
        if command -v curl &> /dev/null; then
            if curl -s -f -o /dev/null "$server_url/health" 2>/dev/null; then
                echo -e "${GREEN}✅ MLflow server is ready!${NC}"
                echo -e "${BLUE}📍 Server URL: $server_url${NC}"
                return 0
            fi
        elif command -v wget &> /dev/null; then
            if wget -q --spider "$server_url/health" 2>/dev/null; then
                echo -e "${GREEN}✅ MLflow server is ready!${NC}"
                echo -e "${BLUE}📍 Server URL: $server_url${NC}"
                return 0
            fi
        else
            # Fallback: check if port is listening
            if command -v nc &> /dev/null; then
                if nc -z "$HOST" "$PORT" 2>/dev/null; then
                    echo -e "${GREEN}✅ MLflow server is ready!${NC}"
                    echo -e "${BLUE}📍 Server URL: $server_url${NC}"
                    return 0
                fi
            fi
        fi

        attempt=$((attempt + 1))
        sleep 0.5
    done

    echo -e "${RED}❌ Timeout: Server did not become ready within 20 seconds${NC}"
    echo -e "${YELLOW}⚠️  Server process (PID: $MLFLOW_PID) may still be starting...${NC}"
    show_log_tail
    return 1
}

# Wait for server to be ready
RED='\033[0;31m'
if wait_for_server; then
    echo "Server is ready"
    echo "export MLFLOW_TRACKING_URI=http://$HOST:$PORT"
    echo "Server logs: ${MLFLOW_LOG_FILE}"
    exit 0
else
    # Server didn't start properly - try to clean up
    ./scripts/stop_mlflow.sh || true
    exit 1
fi
