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
  sed -i.bak 's/8080:8080/3301:8080/' "${DEST}/compose.yaml"
  rm -f "${DEST}/compose.yaml.bak"
fi

# Determine SigNoz authentication mode.
# "impersonation" (default) bypasses the login screen for local dev.
# "login" preserves the upstream login-based auth.
# Override: SIGNOZ_AUTH_MODE=login ./bootstrap-pours.sh
AUTH_MODE_FILE="${ROOT_DIR}/.auth-mode"
AUTH_MODE="${SIGNOZ_AUTH_MODE:-}"

if [[ -z "${AUTH_MODE}" && -f "${AUTH_MODE_FILE}" ]]; then
  AUTH_MODE="$(<"${AUTH_MODE_FILE}")"
fi

AUTH_MODE="${AUTH_MODE:-impersonation}"

COMPOSE="${DEST}/compose.yaml"
DOTENV="${DEST}/.env"

# Inject variable-interpolation placeholders into the freshly downloaded
# compose.yaml so credentials are never hardcoded in a committed file.
if ! grep -q 'SIGNOZ_IDENTN_IMPERSONATION_ENABLED' "${COMPOSE}"; then
  sed -i.bak '/SIGNOZ_TELEMETRYSTORE_PROVIDER=clickhouse/a\
    - SIGNOZ_USER_ROOT_ENABLED=${SIGNOZ_USER_ROOT_ENABLED:-true}\
    - SIGNOZ_USER_ROOT_EMAIL=${SIGNOZ_USER_ROOT_EMAIL:-}\
    - SIGNOZ_USER_ROOT_PASSWORD=${SIGNOZ_USER_ROOT_PASSWORD:-}\
    - SIGNOZ_USER_ROOT_ORG_NAME=${SIGNOZ_USER_ROOT_ORG_NAME:-default}\
    - SIGNOZ_IDENTN_IMPERSONATION_ENABLED=${SIGNOZ_IDENTN_IMPERSONATION_ENABLED:-true}\
    - SIGNOZ_IDENTN_TOKENIZER_ENABLED=${SIGNOZ_IDENTN_TOKENIZER_ENABLED:-false}\
    - SIGNOZ_IDENTN_APIKEY_ENABLED=${SIGNOZ_IDENTN_APIKEY_ENABLED:-false}' "${COMPOSE}"
  rm -f "${COMPOSE}.bak"
fi

if [[ -f "${DOTENV}" ]]; then
  while IFS='=' read -r key value; do
    [[ -n "${key}" && "${key}" != \#* ]] || continue
    if [[ -z "${!key+x}" ]]; then
      export "${key}=${value}"
    fi
  done < "${DOTENV}"
fi

if [[ "${AUTH_MODE}" == "impersonation" ]]; then
  cat > "${DOTENV}" <<DOTENV_CONTENT
SIGNOZ_USER_ROOT_ENABLED=true
SIGNOZ_USER_ROOT_EMAIL=${SIGNOZ_USER_ROOT_EMAIL:-}
SIGNOZ_USER_ROOT_PASSWORD=${SIGNOZ_USER_ROOT_PASSWORD:-}
SIGNOZ_USER_ROOT_ORG_NAME=${SIGNOZ_USER_ROOT_ORG_NAME:-default}
SIGNOZ_IDENTN_IMPERSONATION_ENABLED=true
SIGNOZ_IDENTN_TOKENIZER_ENABLED=false
SIGNOZ_IDENTN_APIKEY_ENABLED=false
DOTENV_CONTENT
else
  cat > "${DOTENV}" <<DOTENV_CONTENT
SIGNOZ_USER_ROOT_ENABLED=true
SIGNOZ_USER_ROOT_EMAIL=${SIGNOZ_USER_ROOT_EMAIL:-}
SIGNOZ_USER_ROOT_PASSWORD=${SIGNOZ_USER_ROOT_PASSWORD:-}
SIGNOZ_USER_ROOT_ORG_NAME=${SIGNOZ_USER_ROOT_ORG_NAME:-default}
SIGNOZ_IDENTN_IMPERSONATION_ENABLED=false
SIGNOZ_IDENTN_TOKENIZER_ENABLED=false
SIGNOZ_IDENTN_APIKEY_ENABLED=false
DOTENV_CONTENT
fi

echo "${AUTH_MODE}" > "${AUTH_MODE_FILE}"

echo "Updated ${DEST} from SigNoz Foundry example."
