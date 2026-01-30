#!/bin/bash

# set -e

# try to kill the service if already running?

PID_FILE="$1"

EXE="$2"

LOGFILE="$3"

PORT="$4"

export GOCOVERDIR="$5"

# set -x

if [[ ! -f "${EXE}" ]]; then
  echo "The service executable ${EXE} does not exist"
  exit 2
fi

# This assumes that the service has already been built, note that the -local flag is passed to the service to run in local mode
${EXE} -local=true > ${LOGFILE} 2>&1 &

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
