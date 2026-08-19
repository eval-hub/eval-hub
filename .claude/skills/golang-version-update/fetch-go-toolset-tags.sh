#!/usr/bin/env bash
# Fetch available go-toolset tags from the Red Hat container registry.
# Handles pagination via Link: rel="next" headers.
# Outputs version-like tags (1.X or 1.X.Y), sorted ascending.
set -euo pipefail

url='https://registry.access.redhat.com/v2/ubi9/go-toolset/tags/list?n=100'
headers_file=$(mktemp)
trap 'rm -f "$headers_file"' EXIT

while [ -n "$url" ]; do
  resp=$(curl -fsS -D "$headers_file" "$url") || {
    echo "error: failed to fetch go-toolset tags from $url" >&2
    exit 1
  }
  if ! echo "$resp" | jq -e 'has("tags") and (.tags | type == "array")' >/dev/null; then
    echo "error: invalid go-toolset tags response (missing tags array) from $url" >&2
    exit 1
  fi
  echo "$resp" | jq -r '.tags[]'
  next=$(grep -i '^link:' "$headers_file" | tr -d '\r' \
    | sed -n 's/.*<\([^>]*\)>; *rel="next".*/\1/p')
  if [ -n "$next" ]; then
    case "$next" in
      http*) url="$next" ;;
      /*) url="https://registry.access.redhat.com${next}" ;;
      *) url="" ;;
    esac
  else
    url=""
  fi
done \
  | grep -E '^1\.[0-9]+(\.[0-9]+)?$' \
  | sort -t. -k1,1n -k2,2n -k3,3n \
  | uniq \
  | tail -20
