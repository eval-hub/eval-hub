#!/usr/bin/env bash
# MCP Protocol Client Library
# Provides JSON-RPC 2.0 helpers for stdio and HTTP/SSE transports.

set -euo pipefail

_MCP_REQUEST_ID=0
_MCP_STDIO_PID=""
_MCP_STDIO_IN=""
_MCP_STDIO_OUT=""
_MCP_TRANSPORT=""  # "stdio" or "http"
_MCP_HTTP_BASE=""
# POST target: defaults to server root (matches evalhub-mcp HTTP handler). Override with EVALHUB_HTTP_PATH=/mcp if needed.
_MCP_HTTP_ENDPOINT=""
_MCP_HTTP_SESSION_ID=""
_MCP_HTTP_TMPDIR=""
_MCP_HTTP_SESSION_FILE=""
_MCP_TMPDIR=""
_MCP_INITIALIZED=false
_MCP_INIT_RESULT=""

mcp_next_id() {
  _MCP_REQUEST_ID=$((_MCP_REQUEST_ID + 1))
  echo "$_MCP_REQUEST_ID"
}

# --- stdio transport -----------------------------------------------------------

mcp_stdio_start() {
  local binary="$1"; shift
  # evalhub-mcp accepts pflag-style arguments only (no subcommands); see cmd/evalhub_mcp/main.go.
  local -a cmd=("$binary" "$@")

  _MCP_TRANSPORT="stdio"
  _MCP_INITIALIZED=false
  _MCP_INIT_RESULT=""
  _MCP_TMPDIR=$(mktemp -d)
  _MCP_STDIO_IN="$_MCP_TMPDIR/stdin.fifo"
  _MCP_STDIO_OUT="$_MCP_TMPDIR/stdout.fifo"
  mkfifo "$_MCP_STDIO_IN" "$_MCP_STDIO_OUT"

  local -a env_args=()
  [[ -n "${EVALHUB_BASE_URL:-}" ]] && env_args+=(EVALHUB_BASE_URL="$EVALHUB_BASE_URL")
  [[ -n "${EVALHUB_TOKEN:-}" ]] && env_args+=(EVALHUB_TOKEN="$EVALHUB_TOKEN")
  [[ -n "${EVALHUB_TENANT:-}" ]] && env_args+=(EVALHUB_TENANT="$EVALHUB_TENANT")
  [[ -n "${EVALHUB_INSECURE:-}" ]] && env_args+=(EVALHUB_INSECURE="$EVALHUB_INSECURE")

  if [[ ${#env_args[@]} -gt 0 ]]; then
    env "${env_args[@]}" "${cmd[@]}" \
      < "$_MCP_STDIO_IN" > "$_MCP_STDIO_OUT" 2>"$_MCP_TMPDIR/stderr.log" &
  else
    "${cmd[@]}" \
      < "$_MCP_STDIO_IN" > "$_MCP_STDIO_OUT" 2>"$_MCP_TMPDIR/stderr.log" &
  fi
  _MCP_STDIO_PID=$!

  exec 3>"$_MCP_STDIO_IN"
  exec 4<"$_MCP_STDIO_OUT"
}

mcp_stdio_stop() {
  if [[ -n "$_MCP_STDIO_PID" ]]; then
    kill "$_MCP_STDIO_PID" 2>/dev/null || true
    wait "$_MCP_STDIO_PID" 2>/dev/null || true
    _MCP_STDIO_PID=""
  fi
  exec 3>&- 2>/dev/null || true
  exec 4<&- 2>/dev/null || true
  [[ -n "$_MCP_TMPDIR" ]] && rm -rf "$_MCP_TMPDIR"
}

mcp_stdio_send() {
  local json="$1"
  printf '%s\n' "$json" >&3
}

mcp_stdio_recv() {
  # One JSON-RPC line per read from fd 4 (stdio server stdout). Uses read -t instead of
  # timeout+head so a single process reads exactly one line with one timer. Spontaneous
  # server notifications still require a queue if they interleave with responses; this
  # harness assumes request/response ordering for each mcp_request.
  local timeout="${1:-${STDIO_TIMEOUT:-10}}"
  local line
  if IFS= read -r -t "$timeout" -u 4 line; then
    printf '%s\n' "$line"
  else
    echo '{"jsonrpc":"2.0","id":null,"error":{"code":-32000,"message":"Request timeout"}}'
  fi
}

# --- HTTP/SSE transport --------------------------------------------------------

mcp_http_wait_ready() {
  local host="${1:-localhost}"
  local port="${2:-3001}"
  local attempt
  for attempt in 1 2 3 4 5; do
    if timeout 2 bash -c "echo >/dev/tcp/${host}/${port}" 2>/dev/null; then
      return 0
    fi
    if curl -sf --max-time 2 "http://${host}:${port}/health" >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done
  return 1
}

mcp_http_start() {
  local host="${1:-localhost}"
  local port="${2:-3001}"
  _MCP_TRANSPORT="http"
  _MCP_HTTP_BASE="http://${host}:${port}"
  _MCP_HTTP_SESSION_ID=""
  _MCP_HTTP_ENDPOINT="${_MCP_HTTP_BASE}${EVALHUB_HTTP_PATH:-}"
  _MCP_HTTP_TMPDIR=$(mktemp -d)
  _MCP_HTTP_SESSION_FILE="$_MCP_HTTP_TMPDIR/session_id"
  : > "$_MCP_HTTP_SESSION_FILE"
  _MCP_INITIALIZED=false
  _MCP_INIT_RESULT=""
  _MCP_REQUEST_ID=0
}

mcp_http_load_session_id() {
  if [[ -z "${_MCP_HTTP_SESSION_ID:-}" && -f "${_MCP_HTTP_SESSION_FILE:-}" ]]; then
    _MCP_HTTP_SESSION_ID=$(cat "$_MCP_HTTP_SESSION_FILE" 2>/dev/null || true)
  fi
}

mcp_http_save_session_id() {
  local sid="$1"
  if [[ -n "$sid" ]]; then
    _MCP_HTTP_SESSION_ID="$sid"
    if [[ -n "${_MCP_HTTP_SESSION_FILE:-}" ]]; then
      printf '%s' "$sid" > "$_MCP_HTTP_SESSION_FILE"
    fi
  fi
}

# Streamable HTTP returns text/event-stream with "data: {jsonrpc...}" lines. jq-based assertions need bare JSON.
mcp_http_strip_sse_data() {
  local raw="$1"
  if printf '%s' "$raw" | grep -q '^data:'; then
    printf '%s' "$raw" | sed -n 's/^data: //p' | tail -n1
  else
    printf '%s' "$raw"
  fi
}

mcp_http_post() {
  local json="$1"
  local timeout="${2:-$HTTP_TIMEOUT}"
  local hdrf bodyf http_code raw sid
  hdrf=$(mktemp "${TMPDIR:-/tmp}/mcp-hdr.XXXXXX")
  bodyf=$(mktemp "${TMPDIR:-/tmp}/mcp-body.XXXXXX")

  mcp_http_load_session_id

  if [[ -z "${_MCP_HTTP_ENDPOINT:-}" ]]; then
    rm -f "$hdrf" "$bodyf"
    echo '{"error":"http_request_failed","detail":"mcp_http_start not called"}'
    return
  fi

  local -a curl_args
  curl_args=(
    -sS
    --max-time "$timeout"
    -D "$hdrf"
    -o "$bodyf"
    -w "%{http_code}"
    -H "Content-Type: application/json"
    -H "Accept: application/json, text/event-stream"
    -d "$json"
  )
  if [[ -n "${_MCP_HTTP_SESSION_ID:-}" ]]; then
    curl_args+=(-H "Mcp-Session-Id: ${_MCP_HTTP_SESSION_ID}")
  fi

  set +e
  http_code=$(curl "${curl_args[@]}" "${_MCP_HTTP_ENDPOINT}" 2>/dev/null)
  local curl_ec=$?
  set -e
  http_code="${http_code//$'\r'/}"
  http_code="${http_code//$'\n'/}"

  if [[ $curl_ec -ne 0 ]]; then
    rm -f "$hdrf" "$bodyf"
    echo '{"error":"http_request_failed","detail":"curl_exit_'${curl_ec}'"}'
    return
  fi

  case "$http_code" in
    200|201|202) ;;
    *)
      rm -f "$hdrf" "$bodyf"
      echo '{"error":"http_request_failed","httpStatus":'"$http_code"'}'
      return
      ;;
  esac

  raw=$(cat "$bodyf")
  rm -f "$bodyf"

  sid=""
  if [[ -f "$hdrf" ]]; then
    sid=$(awk -F': *' 'tolower($1) == "mcp-session-id" { gsub(/\r/, "", $2); print $2; exit }' "$hdrf")
  fi
  rm -f "$hdrf"
  mcp_http_save_session_id "$sid"

  raw=$(mcp_http_strip_sse_data "$raw")
  printf '%s\n' "$raw"
}

# --- Transport-agnostic interface ----------------------------------------------

mcp_request() {
  local method="$1"
  # Use "${2:-"{}"}" — "${2:-{}}" is parsed wrong on bash 3.2 (macOS) and appends a stray "}".
  local params="${2:-"{}"}"
  local id
  id=$(mcp_next_id)

  local json
  json=$(jq -cn \
    --arg method "$method" \
    --argjson params "$params" \
    --argjson id "$id" \
    '{"jsonrpc":"2.0","id":$id,"method":$method,"params":$params}')

  if [[ "$_MCP_TRANSPORT" == "stdio" ]]; then
    mcp_stdio_send "$json"
    mcp_stdio_recv "${STDIO_TIMEOUT:-10}"
  else
    mcp_http_post "$json" "${HTTP_TIMEOUT:-10}"
  fi
}

mcp_notify() {
  local method="$1"
  local params="${2:-"{}"}"

  local json
  json=$(jq -cn \
    --arg method "$method" \
    --argjson params "$params" \
    '{"jsonrpc":"2.0","method":$method,"params":$params}')

  if [[ "$_MCP_TRANSPORT" == "stdio" ]]; then
    mcp_stdio_send "$json"
  else
    mcp_http_post "$json" "${HTTP_TIMEOUT:-10}" >/dev/null
  fi
}

# --- MCP lifecycle helpers -----------------------------------------------------

mcp_initialize() {
  if [[ "${_MCP_INITIALIZED}" == "true" ]]; then
    echo "$_MCP_INIT_RESULT"
    return 0
  fi

  local result
  result=$(mcp_request "initialize" '{
    "protocolVersion": "2024-11-05",
    "capabilities": {},
    "clientInfo": {"name": "evalhub-test-harness", "version": "1.0.0"}
  }')

  if ! echo "$result" | jq -e '.result' >/dev/null 2>&1; then
    echo "$result"
    return 1
  fi

  mcp_http_load_session_id
  if [[ "$_MCP_TRANSPORT" == "http" && -z "${_MCP_HTTP_SESSION_ID:-}" ]]; then
    echo '{"jsonrpc":"2.0","id":0,"error":{"message":"initialize missing Mcp-Session-Id header"}}'
    return 1
  fi

  mcp_notify "notifications/initialized"
  _MCP_INITIALIZED=true
  _MCP_INIT_RESULT="$result"
  echo "$result"
}

# Start transport and complete the MCP initialize handshake (stdio or HTTP).
mcp_setup_transport() {
  local transport="${1:-stdio}"

  if [[ "$transport" == "stdio" ]]; then
    mcp_stdio_start "$EVALHUB_MCP_BIN"
    sleep 1
    if ! kill -0 "$_MCP_STDIO_PID" 2>/dev/null; then
      echo "ERROR: evalhub-mcp stdio process exited immediately" >&2
      if [[ -f "${_MCP_TMPDIR}/stderr.log" ]]; then
        tail -20 "${_MCP_TMPDIR}/stderr.log" >&2
      fi
      return 1
    fi
  else
    mcp_http_start "${EVALHUB_HTTP_HOST:-localhost}" "${EVALHUB_HTTP_PORT:-3001}"
    if ! mcp_http_wait_ready "${EVALHUB_HTTP_HOST:-localhost}" "${EVALHUB_HTTP_PORT:-3001}"; then
      echo "ERROR: MCP HTTP server not reachable at ${EVALHUB_HTTP_HOST:-localhost}:${EVALHUB_HTTP_PORT:-3001}" >&2
      return 1
    fi
  fi

  local result
  result=$(mcp_initialize) || return 1
  if ! echo "$result" | jq -e '.result' >/dev/null 2>&1; then
    echo "ERROR: MCP initialize failed: $(echo "$result" | jq -c '.' 2>/dev/null || echo "$result")" >&2
    return 1
  fi
  return 0
}

mcp_list_resources() {
  mcp_request "resources/list" "${1:-"{}"}"
}

mcp_read_resource() {
  local uri="$1"
  mcp_request "resources/read" "$(jq -cn --arg uri "$uri" '{"uri":$uri}')"
}

mcp_list_tools() {
  mcp_request "tools/list" "${1:-"{}"}"
}

mcp_call_tool() {
  local name="$1"
  local arguments="${2:-"{}"}"
  mcp_request "tools/call" "$(jq -cn --arg name "$name" --argjson arguments "$arguments" '{"name":$name,"arguments":$arguments}')"
}

mcp_list_prompts() {
  mcp_request "prompts/list" "${1:-"{}"}"
}

mcp_get_prompt() {
  local name="$1"
  local arguments="${2:-"{}"}"
  mcp_request "prompts/get" "$(jq -cn --arg name "$name" --argjson arguments "$arguments" '{"name":$name,"arguments":$arguments}')"
}

mcp_complete() {
  local ref_type="$1"  # "ref/resource" or "ref/prompt"
  local ref_uri_or_name="$2"
  local arg_name="$3"
  local arg_value="$4"
  # ref/resource uses "uri" (must not set "name"); ref/prompt uses "name".
  if [[ "$ref_type" == "ref/resource" ]]; then
    mcp_request "completion/complete" "$(jq -cn \
      --arg rt "$ref_type" --arg uri "$ref_uri_or_name" \
      --arg an "$arg_name" --arg av "$arg_value" \
      '{"ref":{"type":$rt,"uri":$uri},"argument":{"name":$an,"value":$av}}')"
  else
    mcp_request "completion/complete" "$(jq -cn \
      --arg rt "$ref_type" --arg rn "$ref_uri_or_name" \
      --arg an "$arg_name" --arg av "$arg_value" \
      '{"ref":{"type":$rt,"name":$rn},"argument":{"name":$an,"value":$av}}')"
  fi
}

mcp_cleanup() {
  if [[ "$_MCP_TRANSPORT" == "stdio" ]]; then
    mcp_stdio_stop
  fi
  if [[ -n "${_MCP_HTTP_TMPDIR:-}" && -d "$_MCP_HTTP_TMPDIR" ]]; then
    rm -rf "$_MCP_HTTP_TMPDIR"
    _MCP_HTTP_TMPDIR=""
    _MCP_HTTP_SESSION_FILE=""
  fi
}

trap mcp_cleanup EXIT
