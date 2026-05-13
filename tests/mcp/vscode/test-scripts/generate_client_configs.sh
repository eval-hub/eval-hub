#!/usr/bin/env bash
# Generates ready-to-paste configuration files for VS Code and Cursor.
# Usage: ./generate_client_configs.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

if [[ -f "$SCRIPT_DIR/test.env" ]]; then
  set -a; source "$SCRIPT_DIR/test.env"; set +a
else
  echo "ERROR: test.env not found. Copy test.env.example and fill in values."
  exit 1
fi

OUT="$SCRIPT_DIR/reports/client-configs"
mkdir -p "$OUT"

BIN="${EVALHUB_MCP_BIN:-/usr/local/bin/evalhub-mcp}"
URL="${EVALHUB_BASE_URL:-https://evalhub.example.com}"
TOKEN="${EVALHUB_TOKEN:-YOUR_TOKEN}"
TENANT="${EVALHUB_TENANT:-YOUR_TENANT}"
HOST="${EVALHUB_HTTP_HOST:-localhost}"
PORT="${EVALHUB_HTTP_PORT:-3001}"

# --- VS Code: stdio -----------------------------------------------------------
cat > "$OUT/vscode-stdio.jsonc" <<EOF
// VS Code settings.json — EvalHub MCP (stdio transport)
// Add inside your user or workspace settings.json
{
  "github.copilot.chat.mcp.servers": {
    "evalhub": {
      "type": "stdio",
      "command": "${BIN}",
      "args": ["mcp"],
      "env": {
        "EVALHUB_TOKEN": "${TOKEN}",
        "EVALHUB_TENANT": "${TENANT}",
        "EVALHUB_BASE_URL": "${URL}"
      }
    }
  }
}
EOF

# --- VS Code: HTTP/SSE --------------------------------------------------------
cat > "$OUT/vscode-http.jsonc" <<EOF
// VS Code settings.json — EvalHub MCP (HTTP/SSE transport)
// First start the server:
//   ${BIN} mcp --transport http --host ${HOST} --port ${PORT}
// Then add to settings.json:
{
  "github.copilot.chat.mcp.servers": {
    "evalhub": {
      "type": "sse",
      "url": "http://${HOST}:${PORT}/sse"
    }
  }
}
EOF

# --- Cursor: stdio -------------------------------------------------------------
cat > "$OUT/cursor-stdio.json" <<EOF
{
  "mcpServers": {
    "evalhub": {
      "command": "${BIN}",
      "args": ["mcp"],
      "env": {
        "EVALHUB_TOKEN": "${TOKEN}",
        "EVALHUB_TENANT": "${TENANT}",
        "EVALHUB_BASE_URL": "${URL}"
      }
    }
  }
}
EOF

# --- Cursor: HTTP/SSE ----------------------------------------------------------
cat > "$OUT/cursor-http.json" <<EOF
{
  "mcpServers": {
    "evalhub": {
      "url": "http://${HOST}:${PORT}/sse"
    }
  }
}
EOF

echo "Generated client configurations in: $OUT/"
echo ""
echo "Files:"
ls -1 "$OUT/"
echo ""
echo "VS Code stdio:  Copy contents of vscode-stdio.jsonc into .vscode/settings.json"
echo "VS Code HTTP:   Start server, then copy vscode-http.jsonc into .vscode/settings.json"
echo "Cursor stdio:   Copy cursor-stdio.json to .cursor/mcp.json (project) or ~/.cursor/mcp.json (global)"
echo "Cursor HTTP:    Start server, then copy cursor-http.json to .cursor/mcp.json"
