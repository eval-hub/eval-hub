#!/usr/bin/env bash
# Regenerate pours/deployment from SigNoz Foundry example compose files.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
DEST="${ROOT_DIR}/pours/deployment"
BASE="https://raw.githubusercontent.com/SigNoz/foundry/main/docs/examples/docker/compose/pours/deployment"

files=(
  compose.yaml
  ingester/ingester.yaml
  ingester/opamp.yaml
  telemetrykeeper/clickhousekeeper/keeper-0.yaml
  telemetrystore/clickhouse/config-0-0.yaml
  telemetrystore/clickhouse/functions.yaml
)

for rel in "${files[@]}"; do
  mkdir -p "${DEST}/$(dirname "${rel}")"
  curl -fsSL "${BASE}/${rel}" -o "${DEST}/${rel}"
done

# Avoid clashing with eval-hub on localhost:8080.
if grep -q '8080:8080' "${DEST}/compose.yaml"; then
  sed -i '' 's/8080:8080/3301:8080/' "${DEST}/compose.yaml"
fi

# Enable impersonation mode so the SigNoz UI loads without a login screen.
COMPOSE="${DEST}/compose.yaml"
if ! grep -q 'SIGNOZ_IDENTN_IMPERSONATION_ENABLED' "${COMPOSE}"; then
  sed -i '' '/SIGNOZ_TELEMETRYSTORE_PROVIDER=clickhouse/a\
    - SIGNOZ_USER_ROOT_ENABLED=true\
    - SIGNOZ_USER_ROOT_EMAIL=admin@local.dev\
    - SIGNOZ_USER_ROOT_PASSWORD=Admin#1234567\
    - SIGNOZ_USER_ROOT_ORG_NAME=default\
    - SIGNOZ_IDENTN_IMPERSONATION_ENABLED=true\
    - SIGNOZ_IDENTN_TOKENIZER_ENABLED=false\
    - SIGNOZ_IDENTN_APIKEY_ENABLED=false' "${COMPOSE}"
fi

echo "Updated ${DEST} from SigNoz Foundry example."
