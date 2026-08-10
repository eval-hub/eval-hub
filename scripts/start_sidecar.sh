#!/bin/bash

# Start the eval-runtime-sidecar in the background.
# Usage: start_sidecar.sh <PID_FILE> <EXE> <LOGFILE> <CONFIG_FILE>
# CONFIG_FILE defaults to config/sidecar_runtime_local.json.
# If the file does not exist a minimal fallback JSON is generated
# (port is derived from base_url by the binary).

PID_FILE="$1"
EXE="$2"
LOGFILE="$3"
CONFIG_FILE="${4:-config/sidecar_runtime_local.json}"

if [[ ! -f "${EXE}" ]]; then
  echo "The sidecar executable ${EXE} does not exist"
  exit 2
fi

if [[ -f "${CONFIG_FILE}" ]]; then
  SIDECAR_JSON="${CONFIG_FILE}"
else
  TMP_JSON="$(mktemp /tmp/sidecar_runtime_XXXXXX.json)" || { echo "Failed to create temp config"; exit 2; }
  chmod 600 "${TMP_JSON}"
  trap 'rm -f "${TMP_JSON}"' EXIT
  printf '{"base_url":"http://localhost:8080","eval_hub":{"base_url":"http://localhost:8080"},"mlflow":{"tracking_uri":"http://localhost:5000"}}\n' > "${TMP_JSON}" || { echo "Failed to write temp config"; exit 2; }
  SIDECAR_JSON="${TMP_JSON}"
fi
"${EXE}" --sidecarconfig "${SIDECAR_JSON}" >> "${LOGFILE}" 2>&1 &
SERVICE_PID=$!
echo "${SERVICE_PID}" > "${PID_FILE}"
sleep 2
echo "Started the sidecar with PID ${SERVICE_PID} (config ${SIDECAR_JSON}), PID file ${PID_FILE}, log ${LOGFILE}"
