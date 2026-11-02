#!/usr/bin/env bash

# Print the AI attribution trailer for the current commit:
#
#   Assisted-by: <full model name> (<harness>)
#
# The model is read from the harness that runs the change, so an agent does not
# have to guess it (agents cannot introspect their own deployment name) or ask
# the author. Resolution order, first match wins:
#
#   1. --model / AI_MODEL
#   2. harness environment: CLINE_MODEL, ANTHROPIC_MODEL, OPENAI_MODEL,
#      GEMINI_MODEL, CURSOR_MODEL
#   3. harness session metadata: Cline ($CLINE_DATA_DIR or ~/.cline/data),
#      Codex CLI ($CODEX_HOME/config.toml), Claude Code
#      (${CLAUDE_CONFIG_DIR:-~/.claude}/settings.json)
#
# The harness name comes from --harness / AI_HARNESS, else from the harness
# environment (CLINE_ACTIVE, CLAUDECODE, CURSOR_TRACE_ID/CURSOR_AGENT,
# CODEX_HOME/CODEX_SANDBOX, COPILOT_AGENT), else from the metadata source that
# supplied the model.
#
# Only the trailer is written to stdout, so it can be piped into a commit
# message.
set -Eeuo pipefail

MODEL="${AI_MODEL:-}"
HARNESS="${AI_HARNESS:-}"
KEEP_PREFIX=false
EXPLAIN=false
MODEL_SOURCE=""
HARNESS_SOURCE=""
METADATA_HARNESS=""

log() {
  printf '[%s] %s\n' "$1" "$2" >&2
}

fail() {
  log FAIL "$1"
  exit 1
}

usage() {
  cat <<'USAGE'
Usage: scripts/assisted-by-trailer.sh [options]

Print the Assisted-by trailer for the current commit, for example:

  Assisted-by: DeepSeek-V4.1-Flash-1017 (Cline)

Options:
  --model <id>     full model identifier to use instead of auto-detection
  --harness <name> harness name to use instead of auto-detection
  --keep-prefix    keep the routing prefix of the model id
                   (rits/deepseek-ai/DeepSeek-V4.1-Flash-1017) instead of
                   printing its last path segment (DeepSeek-V4.1-Flash-1017)
  --explain        report where the model and harness came from (stderr)
  -h, --help       show this help

Environment:
  AI_MODEL           full model identifier (any harness, highest priority)
  AI_HARNESS         harness name (overrides harness detection)
  CLINE_DATA_DIR     Cline data directory (default ~/.cline/data)
  CODEX_HOME         Codex CLI configuration directory (default ~/.codex)
  CLAUDE_CONFIG_DIR  Claude Code configuration directory (default ~/.claude)

Exits non-zero when the model or the harness cannot be determined, so an
automated caller can fall back to asking the author instead of guessing.
USAGE
}

have() {
  command -v "$1" >/dev/null 2>&1
}

# Print a JSON string field: prefer jq with a filter that yields the wanted
# value, and fall back to the last occurrence of the key in the raw file, which
# covers harness metadata written as pretty-printed JSON without requiring jq.
json_field() {
  local file="$1" filter="$2" key="$3" value=""
  if have jq; then
    value="$(jq -r "($filter) // empty" "$file" 2>/dev/null || true)"
  fi
  if [[ -z "$value" ]]; then
    value="$(grep -o "\"$key\"[[:space:]]*:[[:space:]]*\"[^\"]*\"" "$file" 2>/dev/null |
      tail -1 | sed -E 's/.*:[[:space:]]*"([^"]*)"$/\1/' || true)"
  fi
  printf '%s' "$value"
}

model_from_env() {
  local name
  for name in CLINE_MODEL ANTHROPIC_MODEL OPENAI_MODEL GEMINI_MODEL CURSOR_MODEL; do
    if [[ -n "${!name:-}" ]]; then
      MODEL="${!name}"
      MODEL_SOURCE="$name environment variable"
      return 0
    fi
  done
  return 1
}

# Cline records the model this session runs in the session metadata
# (metadata.modelId, rewritten per request) and the configured model in
# settings/providers.json. The newest session file wins so a model switch
# mid-session is reported accurately, but a file caught mid-write is skipped in
# favour of the next session, and the provider settings are the last resort.
model_from_cline() {
  local data="${CLINE_DATA_DIR:-${HOME}/.cline/data}" file
  while IFS= read -r file; do
    MODEL="$(json_field "$file" '.metadata.modelId // .model' modelId)"
    [[ -n "$MODEL" ]] || continue
    MODEL_SOURCE="Cline session metadata ($file)"
    METADATA_HARNESS=Cline
    return 0
  done < <(ls -t "$data"/sessions/*/*.json 2>/dev/null | grep -v '\.messages\.json$' | head -3 || true)
  if [[ -f "$data/settings/providers.json" ]]; then
    MODEL="$(json_field "$data/settings/providers.json" '[.providers[]?.settings.model?] | last' model)"
    if [[ -n "$MODEL" ]]; then
      MODEL_SOURCE="Cline provider settings ($data/settings/providers.json)"
      METADATA_HARNESS=Cline
      return 0
    fi
  fi
  return 1
}

model_from_codex() {
  local config="${CODEX_HOME:-${HOME}/.codex}/config.toml"
  [[ -f "$config" ]] || return 1
  MODEL="$(sed -nE 's/^[[:space:]]*model[[:space:]]*=[[:space:]]*"([^"]+)".*/\1/p' "$config" | tail -1 || true)"
  [[ -n "$MODEL" ]] || return 1
  MODEL_SOURCE="Codex CLI configuration ($config)"
  METADATA_HARNESS="Codex CLI"
}

model_from_claude() {
  local settings="${CLAUDE_CONFIG_DIR:-${HOME}/.claude}/settings.json"
  [[ -f "$settings" ]] || return 1
  MODEL="$(json_field "$settings" '.model' model)"
  [[ -n "$MODEL" ]] || return 1
  MODEL_SOURCE="Claude Code settings ($settings)"
  METADATA_HARNESS="Claude Code"
}

detect_harness() {
  if [[ -n "${CLINE_ACTIVE:-}" ]]; then
    HARNESS=Cline
  elif [[ -n "${CLAUDECODE:-}" ]]; then
    HARNESS="Claude Code"
  elif [[ -n "${CURSOR_TRACE_ID:-}${CURSOR_AGENT:-}" ]]; then
    HARNESS=Cursor
  elif [[ -n "${CODEX_HOME:-}${CODEX_SANDBOX:-}" ]]; then
    HARNESS="Codex CLI"
  elif [[ -n "${COPILOT_AGENT:-}" ]]; then
    HARNESS="GitHub Copilot"
  else
    return 1
  fi
  HARNESS_SOURCE="harness environment"
}

while (($#)); do
  case "$1" in
    --model)
      (($# >= 2)) || fail "--model requires a value"
      MODEL="$2"
      MODEL_SOURCE="--model"
      shift 2
      ;;
    --model=*)
      MODEL="${1#*=}"
      MODEL_SOURCE="--model"
      shift
      ;;
    --harness)
      (($# >= 2)) || fail "--harness requires a value"
      HARNESS="$2"
      HARNESS_SOURCE="--harness"
      shift 2
      ;;
    --harness=*)
      HARNESS="${1#*=}"
      HARNESS_SOURCE="--harness"
      shift
      ;;
    --keep-prefix)
      KEEP_PREFIX=true
      shift
      ;;
    --explain)
      EXPLAIN=true
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      fail "unknown argument: $1 (try --help)"
      ;;
  esac
done

if [[ -z "$MODEL" ]]; then
  MODEL_SOURCE="auto-detection"
  model_from_env || model_from_cline || model_from_codex || model_from_claude || true
fi
[[ -n "$MODEL" ]] ||
  fail "could not determine the model name. Name it explicitly with AI_MODEL='<full model name>' or --model <full model name>; do not guess."

if [[ -z "$HARNESS" ]]; then
  detect_harness || true
fi
if [[ -z "$HARNESS" && -n "$METADATA_HARNESS" ]]; then
  HARNESS="$METADATA_HARNESS"
  HARNESS_SOURCE="metadata source that supplied the model"
fi
[[ -n "$HARNESS" ]] ||
  fail "could not determine the harness name. Name it explicitly with AI_HARNESS='<harness>' or --harness <harness>."

display_model="$MODEL"
if [[ "$KEEP_PREFIX" == false && "$MODEL" == */* ]]; then
  display_model="${MODEL##*/}"
fi

if [[ "$EXPLAIN" == true ]]; then
  log INFO "model: $MODEL (from $MODEL_SOURCE)"
  log INFO "harness: $HARNESS (from ${HARNESS_SOURCE:-auto-detection})"
fi

printf 'Assisted-by: %s (%s)\n' "$display_model" "$HARNESS"
