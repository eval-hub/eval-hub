#!/bin/bash

# Start the evalhub-mcp MCP server in the background.
# Usage: start_mcp.sh <PID_FILE> <EXE> <LOGFILE> <MCP_PORT>

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

if [[ "${CONFIG_FILE}" != "" ]]; then
  "${EXE}" --transport ${TRANSPORT} --port ${MCP_PORT} --config "${CONFIG_FILE}" >> "${LOGFILE}" 2>&1 &
  SERVICE_PID=$!
else
  "${EXE}" --transport ${TRANSPORT} --port ${MCP_PORT} >> "${LOGFILE}" 2>&1 &
  SERVICE_PID=$!
fi

echo "${SERVICE_PID}" > "${PID_FILE}"
sleep 2
echo "Started the MCP server with PID ${SERVICE_PID} (port ${MCP_PORT}), PID file ${PID_FILE}, log ${LOGFILE}" config "${CONFIG_FILE}"
