#!/bin/bash

# Start the eval-runtime-sidecar in the background.
# Usage: start_sidecar.sh <PID_FILE> <EXE> <LOGFILE> <PORT>

PID_FILE="$1"
EXE="$2"
LOGFILE="$3"
PORT="$4"

if [[ ! -f "${EXE}" ]]; then
  echo "The sidecar executable ${EXE} does not exist"
  exit 2
fi

export PORT="${PORT}"
${EXE} >> "${LOGFILE}" 2>&1 &
SERVICE_PID=$!
echo "${SERVICE_PID}" > "${PID_FILE}"
sleep 2
echo "Started the sidecar with PID ${SERVICE_PID} (port ${PORT}), PID file ${PID_FILE}, log ${LOGFILE}"
