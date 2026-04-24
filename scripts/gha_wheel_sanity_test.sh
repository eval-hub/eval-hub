#!/bin/bash
set -euo pipefail

BINARY="eval-hub-server"
CONFIG_DIR="./config"
PORT="${1:-8080}"
FVT_TAGS="${2:---godog.tags=@gha-wheel-sanity}"

LOGFILE=$(mktemp)

cleanup() {
    if [ -n "${SERVER_PID:-}" ]; then
        kill -15 "$SERVER_PID" 2>/dev/null || true
        wait "$SERVER_PID" 2>/dev/null || true
    fi
    rm -f "$LOGFILE"
}
trap cleanup EXIT

# Windows GHA runners lack /tmp; the Go binary resolves it to \tmp on the
# current drive (e.g. D:\tmp) which differs from Git Bash's MSYS2 /tmp
if [[ "$OSTYPE" == msys* ]]; then
    echo "Windows detected (OSTYPE=${OSTYPE}): creating \\tmp via cmd"
    cmd //c "mkdir \\tmp 2>nul" || true
else
    echo "Unix detected (OSTYPE=${OSTYPE}): ensuring /tmp exists"
    mkdir -p /tmp
fi

echo "Starting: ${BINARY} -configdir ${CONFIG_DIR} -local"
"${BINARY}" -configdir "${CONFIG_DIR}" -local > "${LOGFILE}" 2>&1 &
SERVER_PID=$!

SERVER_URL="http://localhost:${PORT}"
HEALTH_URL="${SERVER_URL}/api/v1/health"
MAX_ATTEMPTS=20

echo "Waiting for health at ${HEALTH_URL} ..."
for i in $(seq 1 "$MAX_ATTEMPTS"); do
    if ! kill -0 "$SERVER_PID" 2>/dev/null; then
        echo "Server process died (PID ${SERVER_PID})"
        echo "--- server log ---"
        cat "$LOGFILE"
        echo "--- end ---"
        exit 1
    fi
    RESPONSE=$(curl -s -k --max-time 5 "$HEALTH_URL" 2>/dev/null || true)
    if echo "${RESPONSE}" | grep -qF '"status":"healthy"'; then
        echo "Server is healthy: ${RESPONSE}"
        break
    fi
    if [ "$i" -eq "$MAX_ATTEMPTS" ]; then
        echo "Server failed to become healthy after ${MAX_ATTEMPTS} attempts"
        echo "--- server log ---"
        cat "$LOGFILE"
        echo "--- end ---"
        exit 1
    fi
    echo "  attempt ${i}/${MAX_ATTEMPTS} ..."
    sleep 2
done

echo "Running FVT tests with tags: ${FVT_TAGS}"
SERVER_URL="${SERVER_URL}" FVT_TAGS="${FVT_TAGS}" make test-fvt
