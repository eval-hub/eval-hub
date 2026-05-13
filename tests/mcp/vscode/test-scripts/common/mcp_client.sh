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
_MCP_TMPDIR=""

mcp_next_id() {
  _MCP_REQUEST_ID=$((_MCP_REQUEST_ID + 1))
  echo "$_MCP_REQUEST_ID"
}

# --- stdio transport -----------------------------------------------------------

mcp_stdio_start() {
  local binary="$1"; shift
  _MCP_TRANSPORT="stdio"
  _MCP_TMPDIR=$(mktemp -d)
  _MCP_STDIO_IN="$_MCP_TMPDIR/stdin.fifo"
  _MCP_STDIO_OUT="$_MCP_TMPDIR/stdout.fifo"
  mkfifo "$_MCP_STDIO_IN" "$_MCP_STDIO_OUT"

  "$binary" "$@" < "$_MCP_STDIO_IN" > "$_MCP_STDIO_OUT" 2>"$_MCP_TMPDIR/stderr.log" &
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
  local timeout="${1:-$STDIO_TIMEOUT}"
  timeout "$timeout" head -n1 <&4 2>/dev/null || echo '{"error":"timeout"}'
}

# --- HTTP/SSE transport --------------------------------------------------------

mcp_http_start() {
  local host="${1:-localhost}"
  local port="${2:-3001}"
  _MCP_TRANSPORT="http"
  _MCP_HTTP_BASE="http://${host}:${port}"
  _MCP_HTTP_SESSION_ID=""
  _MCP_HTTP_ENDPOINT="${_MCP_HTTP_BASE}${EVALHUB_HTTP_PATH:-}"
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

  if [[ $curl_ec -ne 0 ]]; then
    rm -f "$hdrf" "$bodyf"
    echo '{"error":"http_request_failed","detail":"curl_exit_'${curl_ec}'"}'
    return
  fi

  case "$http_code" in
    200 | 201 | 202) ;;
    *)
      rm -f "$hdrf" "$bodyf"
      echo '{"error":"http_request_failed","httpStatus":'"$http_code"'}'
      return
      ;;
  esac

  raw=$(cat "$bodyf")
  rm -f "$bodyf"

  sid=$( (grep -i '^mcp-session-id:' "$hdrf" 2>/dev/null || true) | tail -n1 | sed 's/^[^:]*:[[:space:]]*//' | tr -d '\r')
  rm -f "$hdrf"
  if [[ -n "$sid" ]]; then
    _MCP_HTTP_SESSION_ID="$sid"
  fi

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
  local result
  result=$(mcp_request "initialize" '{
    "protocolVersion": "2024-11-05",
    "capabilities": {},
    "clientInfo": {"name": "evalhub-test-harness", "version": "1.0.0"}
  }')
  mcp_notify "notifications/initialized"
  echo "$result"
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
  local ref_name="$2"
  local arg_name="$3"
  local arg_value="$4"
  mcp_request "completion/complete" "$(jq -cn \
    --arg rt "$ref_type" --arg rn "$ref_name" \
    --arg an "$arg_name" --arg av "$arg_value" \
    '{"ref":{"type":$rt,"name":$rn},"argument":{"name":$an,"value":$av}}')"
}

mcp_cleanup() {
  if [[ "$_MCP_TRANSPORT" == "stdio" ]]; then
    mcp_stdio_stop
  fi
}

trap mcp_cleanup EXIT
