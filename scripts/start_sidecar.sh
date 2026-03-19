#!/bin/bash

# Start the eval-runtime-sidecar in the background.
# Usage: start_sidecar.sh <PID_FILE> <EXE> <LOGFILE> <SIDECAR_PORT> <CONFIG_DIR>
# CONFIG_DIR defaults to repo config/; uses sidecar_runtime_local.json if present,
# else writes a minimal JSON using SIDECAR_PORT.

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
SIDECAR_JSON="${CONFIG_DIR}/sidecar_runtime_local.json"
if [[ -f "${SIDECAR_JSON}" ]]; then
  :
else
  TMP_JSON="/tmp/sidecar_runtime_$$.json"
  PORT="${SIDECAR_PORT:-8080}"
  printf '{"port":%s,"base_url":"http://localhost:%s","eval_hub":{"base_url":"http://localhost:8080"},"mlflow":{"tracking_uri":"http://localhost:5000"}}\n' "${PORT}" "${PORT}" > "${TMP_JSON}"
  SIDECAR_JSON="${TMP_JSON}"
fi
"${EXE}" --sidecarconfig "${SIDECAR_JSON}" >> "${LOGFILE}" 2>&1 &
SERVICE_PID=$!
echo "${SERVICE_PID}" > "${PID_FILE}"
sleep 2
echo "Started the sidecar with PID ${SERVICE_PID} (port ${SIDECAR_PORT}, config ${CONFIG_DIR}), PID file ${PID_FILE}, log ${LOGFILE}"
