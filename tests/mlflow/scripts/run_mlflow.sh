#!/bin/bash

# Script to run MLflow server locally
# Default configuration: runs on http://localhost:5000

set -euo pipefail

# Default values
HOST=${MLFLOW_HOST:-"127.0.0.1"}
PORT=${MLFLOW_PORT:-"5000"}
BACKEND_URI=${MLFLOW_BACKEND_STORE_URI:-"sqlite:///mlflow.db"}
DEFAULT_ARTIFACT_ROOT=${MLFLOW_DEFAULT_ARTIFACT_ROOT:-"./mlruns"}

# Colors for output
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${BLUE}🚀 Starting MLflow server...${NC}"
echo ""
echo -e "Configuration:"
echo -e "  ${YELLOW}Host:${NC} $HOST"
echo -e "  ${YELLOW}Port:${NC} $PORT"
echo -e "  ${YELLOW}Backend Store URI:${NC} $BACKEND_URI"
echo -e "  ${YELLOW}Default Artifact Root:${NC} $DEFAULT_ARTIFACT_ROOT"
echo ""

# Check if MLflow is installed
if ! command -v mlflow &> /dev/null; then
    echo -e "${YELLOW}⚠️  MLflow not found. Installing...${NC}"
    ./scripts/download_mlflow.sh
    echo ""
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

echo -e "${GREEN}✅ Starting MLflow server...${NC}"
MLFLOW_VERSION=$(mlflow --version 2>/dev/null | head -n 1)
echo -e "${BLUE}📍 MLflow version: ${MLFLOW_VERSION}${NC}"
echo -e "${BLUE}📍 Server will be available at: http://$HOST:$PORT${NC}"
echo -e "${YELLOW}💡 Press Ctrl+C to stop the server${NC}"
echo ""

mkdir -p bin

# Start MLflow server in background
mlflow server \
    --host "$HOST" \
    --port "$PORT" \
    --backend-store-uri "$BACKEND_URI" \
    --default-artifact-root "$DEFAULT_ARTIFACT_ROOT" | tee bin/mlflow.log &

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
    return 1
}

# Wait for server to be ready
RED='\033[0;31m'
if wait_for_server; then
    # Server is ready - if running in background mode, exit successfully
    # The server will continue running in the background
    echo "Server is ready"
    # exit 0
else
    # Server didn't start properly - try to clean up
    ./scripts/stop_mlflow.sh || true
    exit 1
fi
