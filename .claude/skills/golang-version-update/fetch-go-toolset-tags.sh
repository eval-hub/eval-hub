#!/usr/bin/env bash
# Fetch available go-toolset tags from the Red Hat container registry.
# Handles pagination via Link: rel="next" headers.
# Outputs version-like tags (1.X or 1.X.Y), sorted ascending.
set -euo pipefail

readonly REGISTRY_ORIGIN='https://registry.access.redhat.com'
readonly MAX_PAGES=50
readonly CURL_CONNECT_TIMEOUT=10
readonly CURL_MAX_TIME=30

url="${REGISTRY_ORIGIN}/v2/ubi9/go-toolset/tags/list?n=100"
headers_file=$(mktemp)
trap 'rm -f "$headers_file"' EXIT

page=0
declare -A seen_urls

while [ -n "$url" ]; do
  if (( page >= MAX_PAGES )); then
    echo "error: exceeded maximum page count (${MAX_PAGES}); aborting" >&2
    exit 1
  fi

  if [[ -n "${seen_urls[$url]+_}" ]]; then
    echo "error: pagination cycle detected at $url" >&2
    exit 1
  fi
  seen_urls[$url]=1

  resp=$(curl -fsS \
    --connect-timeout "$CURL_CONNECT_TIMEOUT" \
    --max-time "$CURL_MAX_TIME" \
    -D "$headers_file" "$url") || {
    echo "error: failed to fetch go-toolset tags from $url" >&2
    exit 1
  }

  if ! echo "$resp" | jq -e 'has("tags") and (.tags | type == "array")' >/dev/null; then
    echo "error: invalid go-toolset tags response (missing tags array) from $url" >&2
    exit 1
  fi

  if ! echo "$resp" | jq -e '.tags | all(type == "string")' >/dev/null; then
    echo "error: tags array contains non-string elements from $url" >&2
    exit 1
  fi

  echo "$resp" | jq -r '.tags[] | select(test("^1\\.[0-9]+(\\.[0-9]+)?$"))'

  next=$({ grep -i '^link:' "$headers_file" || true; } | tr -d '\r' \
    | sed -n 's/.*<\([^>]*\)>; *rel="next".*/\1/p')
  if [ -n "$next" ]; then
    case "$next" in
      "${REGISTRY_ORIGIN}"/*) url="$next" ;;
      /*) url="${REGISTRY_ORIGIN}${next}" ;;
      *)
        echo "error: pagination next URL points outside registry origin: $next" >&2
        exit 1
        ;;
    esac
  else
    url=""
  fi

  (( ++page ))
done \
  | sort -t. -k1,1n -k2,2n -k3,3n \
  | uniq
