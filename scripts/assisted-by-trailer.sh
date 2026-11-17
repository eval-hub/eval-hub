#!/usr/bin/env bash

# Print the AI attribution trailer for the current commit:
#
#   Assisted-by: <full model name> (<harness>)
#
# The model is read from the harness that runs the change, so an agent does not
# have to guess it (no harness lets an agent read its own deployment name) or ask
# the author. This is harness-agnostic: it covers Cline, Codex CLI, Claude Code,
# Cursor and any harness that exports a model variable. The harness is resolved
# first, so one harness's on-disk state is never reported for a change made in
# another. Resolution order, first match wins:
#
#   1. --model / AI_MODEL
#   2. harness environment, harness-specific variables first: CLINE_MODEL,
#      CODEX_MODEL / OPENAI_MODEL, CLAUDE_MODEL / ANTHROPIC_MODEL, CURSOR_MODEL
#   3. the detected harness's metadata: Cline ($CLINE_DATA_DIR or ~/.cline/data),
#      Codex CLI ($CODEX_HOME or ~/.codex/config.toml), Claude Code
#      (${CLAUDE_CONFIG_DIR:-~/.claude}/settings.json)
#
# The harness name comes from --harness / AI_HARNESS, else from the harness
# environment (CLINE_ACTIVE, CLAUDECODE, CURSOR_TRACE_ID/CURSOR_AGENT,
# CODEX_HOME/CODEX_SANDBOX, COPILOT_AGENT), else from the metadata source that
# supplied the model when exactly one harness reports one.
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

Print the Assisted-by trailer for the current commit, in the harness that runs
the command, for example:

  Assisted-by: DeepSeek-V4.1-Flash-1017 (Cline)
  Assisted-by: gpt-6.1-sol (Codex CLI)

Options:
  --model <id>     full model identifier to use instead of auto-detection
  --harness <name> harness name to use instead of auto-detection
  --keep-prefix    keep the routing prefix of the model id
                   (vendor/path/Model-Id) instead of printing its last path
                   segment (Model-Id)
  --explain        report where the model and harness came from (stderr)
  -h, --help       show this help

Environment:
  AI_MODEL           full model identifier (any harness, highest priority)
  AI_HARNESS         harness name (overrides harness detection)
  CLINE_MODEL        model variable exported by the Cline harness
  CODEX_MODEL        model variable exported by the Codex CLI harness
  CLAUDE_MODEL       model variable exported by the Claude Code harness
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

model_env_candidates() {
  case "${1:-}" in
    Cline) printf '%s\n' CLINE_MODEL ANTHROPIC_MODEL OPENAI_MODEL ;;
    "Codex CLI") printf '%s\n' CODEX_MODEL OPENAI_MODEL ANTHROPIC_MODEL ;;
    "Claude Code") printf '%s\n' CLAUDE_MODEL ANTHROPIC_MODEL OPENAI_MODEL ;;
    Cursor) printf '%s\n' CURSOR_MODEL ANTHROPIC_MODEL OPENAI_MODEL ;;
    *) printf '%s\n' CLINE_MODEL CODEX_MODEL ANTHROPIC_MODEL CLAUDE_MODEL \
      OPENAI_MODEL GEMINI_MODEL CURSOR_MODEL ;;
  esac
}

# Environment variables are checked harness-first, so a model another agent on the
# same machine exported cannot be reported for this change.
model_from_env() {
  local name
  while IFS= read -r name; do
    if [[ -n "${!name:-}" ]]; then
      MODEL="${!name}"
      MODEL_SOURCE="$name environment variable"
      return 0
    fi
  done < <(model_env_candidates "${1:-}")
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

# Read the model from the metadata of the harness that is running this change.
# Cursor keeps no documented on-disk model record, so it relies on the
# environment (or --model).
model_from_harness_metadata() {
  case "${1:-}" in
    Cline) model_from_cline ;;
    "Codex CLI") model_from_codex ;;
    "Claude Code") model_from_claude ;;
    *) return 1 ;;
  esac
}

# Print "<harness>\t<model>\t<source>" for each metadata source that reports a
# model on this machine, so an unknown harness can be resolved only when exactly
# one harness has state to report.
metadata_candidates() {
  local reader
  for reader in cline codex claude; do
    (
      MODEL=""
      METADATA_HARNESS=""
      MODEL_SOURCE=""
      "model_from_$reader" >/dev/null 2>&1 || exit 0
      [[ -n "$MODEL" ]] || exit 0
      printf '%s\t%s\t%s\n' "$METADATA_HARNESS" "$MODEL" "$MODEL_SOURCE"
    )
  done
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

# Resolve the harness first: the harness that runs this change decides which
# environment variables and which metadata may be read, so state left behind by
# another agent on the same machine is never attributed to this change.
if [[ -z "$HARNESS" ]]; then
  detect_harness || true
fi

if [[ -z "$MODEL" ]]; then
  MODEL_SOURCE="auto-detection"
  if [[ -n "$HARNESS" ]]; then
    model_from_env "$HARNESS" || model_from_harness_metadata "$HARNESS" || true
  else
    model_from_env "" || true
    if [[ -z "$MODEL" ]]; then
      candidate_count=0
      while IFS=$'\t' read -r candidate_harness candidate_model candidate_source; do
        [[ -n "$candidate_model" ]] || continue
        candidate_count=$((candidate_count + 1))
        MODEL="$candidate_model"
        MODEL_SOURCE="$candidate_source"
        METADATA_HARNESS="$candidate_harness"
      done < <(metadata_candidates)
      ((candidate_count <= 1)) ||
        fail "several harnesses report a model on this machine; pick one with AI_HARNESS='<harness>' or --harness <harness>."
    fi
  fi
fi
[[ -n "$MODEL" ]] ||
  fail "could not determine the model name. Name it explicitly with AI_MODEL='<full model name>' or --model <full model name>; do not guess."

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
