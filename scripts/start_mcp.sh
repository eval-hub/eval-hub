#!/bin/bash

# Start the evalhub-mcp MCP server in the background.
# Usage: start_mcp.sh <PID_FILE> <EXE> <LOGFILE> <MCP_PORT> [CONFIG_FILE]

PID_FILE="$1"
EXE="$2"
LOGFILE="$3"
MCP_PORT="$4"
CONFIG_FILE="${5}"
TRANSPORT="http"

if [[ ! -f "${EXE}" ]]; then
  echo "The MCP server executable ${EXE} does not exist"
  exit 2
fi

run_mcp() {
  # When using a config file, ignore stale EVALHUB_* from the shell (e.g. a bad test.env export).
  # Unset in this script (not a subshell) so SERVICE_PID=$! is the MCP process for PID_FILE and stop_server.sh.
  if [[ "${CONFIG_FILE}" != "" ]]; then
    unset EVALHUB_BASE_URL EVALHUB_TOKEN EVALHUB_TENANT EVALHUB_INSECURE
    unset EVALHUB_TRANSPORT EVALHUB_HOST EVALHUB_PORT EVALHUB_LIST_PAGE_LIMIT
    "${EXE}" --transport "${TRANSPORT}" --port "${MCP_PORT}" --config "${CONFIG_FILE}" >> "${LOGFILE}" 2>&1 &
  else
    "${EXE}" --transport "${TRANSPORT}" --port "${MCP_PORT}" >> "${LOGFILE}" 2>&1 &
  fi
  SERVICE_PID=$!
}

run_mcp

echo "${SERVICE_PID}" > "${PID_FILE}"
sleep 3
if ! kill -0 "${SERVICE_PID}" 2>/dev/null; then
  echo "ERROR: evalhub-mcp exited immediately (see ${LOGFILE})" >&2
  tail -20 "${LOGFILE}" >&2 || true
  exit 2
fi
echo "Started the MCP server with PID ${SERVICE_PID} (port ${MCP_PORT}), PID file ${PID_FILE}, log ${LOGFILE} config ${CONFIG_FILE}"
