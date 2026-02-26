#!/bin/bash

# set -e

# try to kill the service if already running?
#
# Usage: start_server.sh PID_FILE EXE LOGFILE PORT GOCOVERDIR [mock]
#   Optional 6th arg: "mock" (or "1"/"true") to start in mock K8s mode (KUBE_MOCK_ENABLED=1).
#   Otherwise mock mode is enabled only if KUBE_MOCK_ENABLED is already set in the environment.
#   The executable should be built with -tags=mock for mock mode to use the mock helper.

PID_FILE="$1"
EXE="$2"
LOGFILE="$3"
PORT="$4"
export GOCOVERDIR="$5"
MOCK_ARG="${6:-}"

# set -x

if [[ ! -f "${EXE}" ]]; then
  echo "The service executable ${EXE} does not exist"
  exit 2
fi

# Enable mock K8s mode if 6th argument is "mock"/"1"/"true" or KUBE_MOCK_ENABLED is already set.
if [[ "${MOCK_ARG}" == "mock" || "${MOCK_ARG}" == "1" || "${MOCK_ARG}" == "true" ]]; then
  export KUBE_MOCK_ENABLED=1
fi

# If KUBE_MOCK_ENABLED is true, run without --local (K8s runtime with mock helper).
# Otherwise run with --local (local mode, CORS enabled).
EXTRA_ARGS=""
if [[ "${KUBE_MOCK_ENABLED}" != "true" && "${KUBE_MOCK_ENABLED}" != "1" ]]; then
  EXTRA_ARGS="--local"
fi
${EXE} ${EXTRA_ARGS} > ${LOGFILE} 2>&1 &

SERVICE_PID=$!

echo "${SERVICE_PID}" > "${PID_FILE}"

CURL_OPTS="-k -s"
SERVER_URL="http://localhost:${PORT}/api/v1/health"

# Now wait for the service to start
waiting=true
count=0
maxCount=20
while [[ "${waiting}" == "true" ]];do
  echo "Trying heartbeat (curl ${CURL_OPTS} ${SERVER_URL}) ..."
  # this is not configurable
  response=$(curl ${CURL_OPTS} ${SERVER_URL})
  if [[ "${response}" == *'"status":"healthy"'* ]]; then
    waiting=false
    echo "${response}"
  else
    # for debugging issues
    # echo "Response: ${response}"
    sleep 2
  fi
  count=$((count+1))
  if [[ ${count} -gt ${maxCount} ]]; then
    echo "Failing the wait for the service due to too many attempts ${count}"
    # show the repo service log in case the error comes from a bad startup (missing configuration etc)
    if [[ -f "${LOGFILE}" ]]; then
      echo "Service log: ${LOGFILE}"
      echo "--------------------------------"
      cat "${LOGFILE}"
      echo "--------------------------------"
    fi
    exit 2
  fi
done

echo "Started the repo service with PID ${SERVICE_PID} stored in file ${PID_FILE}"
