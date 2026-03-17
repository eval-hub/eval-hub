#!/bin/bash

# Start the eval-runtime-sidecar in the background.
# Usage: start_sidecar.sh <PID_FILE> <EXE> <LOGFILE> <SIDECAR_PORT> <CONFIG_DIR>
# SIDECAR_PORT is exported so config (env_mappings: SIDECAR_PORT -> sidecar.port) applies.
# CONFIG_DIR is passed as --configdir and exported as EVAL_HUB_CONFIG_DIR.

PID_FILE="$1"
EXE="$2"
LOGFILE="$3"
SIDECAR_PORT="$4"
CONFIG_DIR="${5:-config}"

if [[ ! -f "${EXE}" ]]; then
  echo "The sidecar executable ${EXE} does not exist"
  exit 2
fi

export SIDECAR_PORT="${SIDECAR_PORT}"
export EVAL_HUB_CONFIG_DIR="${CONFIG_DIR}"
"${EXE}" --configdir "${CONFIG_DIR}" >> "${LOGFILE}" 2>&1 &
SERVICE_PID=$!
echo "${SERVICE_PID}" > "${PID_FILE}"
sleep 2
echo "Started the sidecar with PID ${SERVICE_PID} (port ${SIDECAR_PORT}, config ${CONFIG_DIR}), PID file ${PID_FILE}, log ${LOGFILE}"
