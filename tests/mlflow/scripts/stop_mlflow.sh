#!/bin/bash

# Stop the MLflow server for MLFLOW_PORT only (default 5000).
# Avoids killing other port-specific servers used by parallel/sequential integration cases.
# Only processes verified as this port's MLflow server are signalled. A port probe
# cannot reserve the port, so by the time this runs an unrelated process may hold it;
# that process is reported and left running instead of being killed.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MLFLOW_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
cd "${MLFLOW_DIR}"
mkdir -p bin

PORT="${MLFLOW_PORT:-5000}"
case "${PORT}" in
    '' | *[!0-9]*)
        echo "❌ MLFLOW_PORT must be a port number, got '${PORT}'"
        exit 2
        ;;
esac
PID_FILE="bin/mlflow_${PORT}.pid"

# ERE matching an MLflow server invocation and nothing else: the `mlflow` console
# script (`mlflow server …`, or `<venv>/bin/python <venv>/bin/mlflow server …` from
# its shebang) or `python -m mlflow server …`. It is anchored at the start of the
# command line, so a command that merely mentions mlflow (a log path, a file name,
# a grep pattern) never matches. Shared with pgrep/pkill -f, which take this ERE.
MLFLOW_SERVER_RE='^([^[:space:]]*/)?([Pp]ython[0-9.]*[[:space:]]+([^[:space:]]*/|-m[[:space:]]+))?mlflow[[:space:]]+server([[:space:]]|$)'

# ERE matching this port's flag on that server: `--port 5010`, `--port=5010`,
# `-b host:5010`, `--bind host:5010` or their `=` forms.
MLFLOW_PORT_RE='(--port(=|[[:space:]])|(-b|--bind)(=|[[:space:]])[^[:space:]]*:)'"${PORT}"'([[:space:]]|$)'

# Combined ERE for the full command line, as matched by pgrep -f and pkill -f.
MLFLOW_SERVER_PID_RE="${MLFLOW_SERVER_RE}.*${MLFLOW_PORT_RE}"

echo "🛑 Stopping MLflow server on port ${PORT}..."

kill_pid() {
    local pid="$1"
    if [ -z "${pid}" ]; then
        return 0
    fi
    if kill -0 "${pid}" 2>/dev/null; then
        kill "${pid}" 2>/dev/null || true
    fi
}

# command_line prints the command line for PID (empty when the process is gone).
command_line() {
    ps -p "$1" -o command= 2>/dev/null || true
}

# parent_pid prints the parent PID for PID (empty when it has none).
parent_pid() {
    ps -p "$1" -o ppid= 2>/dev/null | tr -d '[:space:]' || true
}

# is_port_mlflow_command reports whether a command line is this port's MLflow
# server. The command must be an MLflow server invocation (see MLFLOW_SERVER_RE),
# with the port accepted as `--port 5010`, `--port=5010`, `-b host:5010`,
# `--bind host:5010` or their `=` forms.
is_port_mlflow_command() {
    local command="$1"
    [[ "${command}" =~ ${MLFLOW_SERVER_RE} ]] || return 1
    [[ "${command}" =~ ${MLFLOW_PORT_RE} ]]
}

# is_verified_mlflow_pid reports whether PID, or one of its ancestors, is this
# port's MLflow server. uvicorn/gunicorn workers are forked after `mlflow server`
# starts them, so their own command line no longer names the port; the ancestor
# chain is what identifies them.
is_verified_mlflow_pid() {
    local pid="$1" depth=0
    while [ "${depth}" -lt 8 ]; do
        case "${pid}" in
            '' | *[!0-9]*) return 1 ;;
        esac
        if [ "${pid}" -le 1 ]; then
            return 1
        fi
        if is_port_mlflow_command "$(command_line "${pid}")"; then
            return 0
        fi
        pid="$(parent_pid "${pid}")"
        depth=$((depth + 1))
    done
    return 1
}

# listeners prints the PIDs listening on PORT (empty when lsof is unavailable).
listeners() {
    if ! command -v lsof >/dev/null 2>&1; then
        return 0
    fi
    lsof -tiTCP:"${PORT}" -sTCP:LISTEN 2>/dev/null || true
}

# mlflow_server_running reports whether a process running this port's MLflow server
# invocation is still alive. It is the liveness test used when lsof is unavailable
# and when a forced stop is confirmed; pgrep is optional, so the test is skipped
# without it.
mlflow_server_running() {
    if ! command -v pgrep >/dev/null 2>&1; then
        return 1
    fi
    pgrep -f "${MLFLOW_SERVER_PID_RE}" >/dev/null 2>&1
}

# FOREIGN_PIDS collects listeners that are not this port's MLflow server. They are
# reported rather than signalled, so a process that grabbed the port after the
# availability probe is never killed.
FOREIGN_PIDS=""

# kill_listeners signals verified MLflow listeners on PORT with SIGNAL
# (-TERM by default). Verified PIDs are collected before anything is signalled:
# a worker is verified through its ancestors, so killing the parent first would
# break that chain for the remaining PIDs.
kill_listeners() {
    local signal="${1:--TERM}" pid verified=""
    for pid in $(listeners); do
        if is_verified_mlflow_pid "${pid}"; then
            verified="${verified} ${pid}"
        else
            FOREIGN_PIDS="${FOREIGN_PIDS} ${pid}"
            echo "⚠️  Port ${PORT} is held by PID ${pid} ($(command_line "${pid}")), which is not this port's MLflow server; leaving it running"
        fi
    done
    if [ -n "${verified}" ]; then
        # shellcheck disable=SC2086 # deliberate word splitting of the PID list
        kill "${signal}" ${verified} 2>/dev/null || true
    fi
}

# refuse_foreign_listeners exits when the port belongs to a process we must not kill.
refuse_foreign_listeners() {
    if [ -z "${FOREIGN_PIDS}" ]; then
        return 0
    fi
    echo "❌ Port ${PORT} is held by a process that is not this port's MLflow server (PIDs:${FOREIGN_PIDS}); refusing to kill it"
    exit 1
}

if [ -f "${PID_FILE}" ]; then
    PID="$(tr -d '[:space:]' < "${PID_FILE}" || true)"
    if [ -n "${PID}" ]; then
        if is_verified_mlflow_pid "${PID}"; then
            kill_pid "${PID}"
            # MLflow/uvicorn may leave children listening after the parent exits.
            if command -v pkill >/dev/null 2>&1; then
                pkill -P "${PID}" 2>/dev/null || true
            fi
        else
            echo "⚠️  Recorded PID ${PID} is not this port's MLflow server any more; skipping it"
        fi
    fi
    rm -f "${PID_FILE}"
fi

# Free anything still listening on this port (parent or worker) — verified MLflow only.
kill_listeners

# Narrow fallback: only an MLflow server invocation for this port (never all
# mlflow.server processes, and never a command that merely mentions mlflow).
if command -v pkill >/dev/null 2>&1; then
    pkill -f "${MLFLOW_SERVER_PID_RE}" 2>/dev/null || true
fi

refuse_foreign_listeners

timeout=50
iterations=0
while [ "${iterations}" -lt "${timeout}" ]; do
    still_listening=0
    if command -v lsof >/dev/null 2>&1; then
        if [ -n "$(listeners)" ]; then
            still_listening=1
        fi
    elif mlflow_server_running; then
        still_listening=1
    fi
    if [ "${still_listening}" -eq 0 ]; then
        echo "🛑 MLflow server on port ${PORT} stopped"
        exit 0
    fi
    sleep 0.1
    iterations=$((iterations + 1))
done

# Last resort after graceful attempts timed out: force only verified listeners.
kill_listeners -9
refuse_foreign_listeners
# A process can stop listening without having exited, so the forced stop only
# counts as a success once no listener and no matching MLflow process remains.
if [ -z "$(listeners)" ] && ! mlflow_server_running; then
    echo "🛑 MLflow server on port ${PORT} stopped (forced)"
    exit 0
fi

echo "❌ MLflow server on port ${PORT} is still running after 5 seconds"
exit 1
