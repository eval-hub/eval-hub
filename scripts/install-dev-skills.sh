#!/usr/bin/env bash

# Install the eval-hub development skills for the agent harness in use.
#
# The skills themselves live in the external repository
# https://github.com/eval-hub/eval-hub-skills (dev-skills/) so that they can be
# shared by every eval-hub repository instead of being copied around. This
# script keeps a shallow clone of that repository up to date and symlinks each
# development skill into the harness skills directory (default:
# ~/.claude/skills).
#
#   make install-dev-skills
#   make uninstall-dev-skills
#
# Environment overrides:
#   EVALHUB_SKILLS_REPO_URL  git remote (default: the eval-hub-skills GitHub repo)
#   EVALHUB_SKILLS_REF       branch, tag, or commit (default: main)
#   EVALHUB_SKILLS_DIR       shallow clone location (default: ~/.cache/eval-hub-skills)
#   CLAUDE_SKILLS_DIR        symlink target (default: ~/.claude/skills)
set -Eeuo pipefail

REPO_URL="${EVALHUB_SKILLS_REPO_URL:-https://github.com/eval-hub/eval-hub-skills.git}"
REF="${EVALHUB_SKILLS_REF:-main}"
CHECKOUT_DIR="${EVALHUB_SKILLS_DIR:-${XDG_CACHE_HOME:-$HOME/.cache}/eval-hub-skills}"
SKILLS_DIR="${CLAUDE_SKILLS_DIR:-$HOME/.claude/skills}"
UNINSTALL=false
FORCE=false
SKILLS=(evalhub-bug-review fix-fvt-test golang-toolchain-update release-notes)

log() {
  printf '[%s] %s\n' "$1" "$2" >&2
}

fail() {
  log FAIL "$1"
  exit 1
}

usage() {
  cat <<'USAGE'
Usage: scripts/install-dev-skills.sh [options]

Symlink the eval-hub development skills from
https://github.com/eval-hub/eval-hub-skills (dev-skills/) into the harness
skills directory, keeping a shallow clone of that repository up to date.

Options:
  --uninstall      remove the skill symlinks instead of creating them
  --force          replace an existing directory at a link target
  --ref <ref>      branch, tag, or commit to check out (default: main)
  --dir <path>     shallow clone location (default: ~/.cache/eval-hub-skills)
  --target <path>  harness skills directory (default: ~/.claude/skills)
  -h, --help       show this help

Environment: EVALHUB_SKILLS_REPO_URL, EVALHUB_SKILLS_REF, EVALHUB_SKILLS_DIR,
CLAUDE_SKILLS_DIR.
USAGE
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --uninstall) UNINSTALL=true ;;
    --force) FORCE=true ;;
    --ref)
      [ "$#" -ge 2 ] || fail "--ref requires a value"
      REF="$2"
      shift
      ;;
    --dir)
      [ "$#" -ge 2 ] || fail "--dir requires a value"
      CHECKOUT_DIR="$2"
      shift
      ;;
    --target)
      [ "$#" -ge 2 ] || fail "--target requires a value"
      SKILLS_DIR="$2"
      shift
      ;;
    -h | --help)
      usage
      exit 0
      ;;
    *)
      usage
      fail "unknown argument: $1"
      ;;
  esac
  shift
done

fetch_ref() {
  if ! git -C "$CHECKOUT_DIR" fetch --depth 1 --quiet origin "$REF"; then
    fail "unable to fetch '$REF' from $REPO_URL; check --ref / EVALHUB_SKILLS_REF"
  fi
  git -C "$CHECKOUT_DIR" checkout --quiet --force FETCH_HEAD
}

sync_checkout() {
  if [ -e "$CHECKOUT_DIR" ] && [ ! -d "$CHECKOUT_DIR/.git" ]; then
    fail "$CHECKOUT_DIR exists but is not a git checkout; remove it or pass --dir"
  fi

  if [ -d "$CHECKOUT_DIR/.git" ]; then
    log INFO "Updating $CHECKOUT_DIR ($REF)"
    fetch_ref
  else
    log INFO "Cloning $REPO_URL into $CHECKOUT_DIR"
    mkdir -p "$(dirname "$CHECKOUT_DIR")"
    git clone --quiet --depth 1 "$REPO_URL" "$CHECKOUT_DIR" ||
      fail "unable to clone $REPO_URL"
    fetch_ref
  fi

  local skill
  for skill in "${SKILLS[@]}"; do
    [ -f "$CHECKOUT_DIR/dev-skills/$skill/SKILL.md" ] ||
      fail "dev-skills/$skill/SKILL.md is missing in $CHECKOUT_DIR; is '$REF' a valid ref?"
  done
}

install_skills() {
  mkdir -p "$SKILLS_DIR"

  local installed=0
  local skipped=0
  local skill src dst
  for skill in "${SKILLS[@]}"; do
    src="$CHECKOUT_DIR/dev-skills/$skill"
    dst="$SKILLS_DIR/$skill"
    if [ -L "$dst" ]; then
      rm -f "$dst"
    elif [ -e "$dst" ]; then
      if [ "$FORCE" != true ]; then
        log SKIP "$dst exists and is not a symlink; rerun with --force to replace it"
        skipped=$((skipped + 1))
        continue
      fi
      rm -rf "$dst"
    fi
    ln -s "$src" "$dst"
    log OK "$dst -> $src"
    installed=$((installed + 1))
  done

  log INFO "Linked $installed skill(s) into $SKILLS_DIR, skipped $skipped"
  log INFO "Invoke a skill by name, e.g. Claude Code: /release-notes; Cursor/Codex: @$CHECKOUT_DIR/dev-skills/release-notes/SKILL.md"
}

uninstall_skills() {
  local removed=0
  local skill dst target
  for skill in "${SKILLS[@]}"; do
    dst="$SKILLS_DIR/$skill"
    if [ -L "$dst" ]; then
      target="$(readlink "$dst" || true)"
      case "$target" in
        */dev-skills/"$skill")
          rm -f "$dst"
          log OK "removed $dst"
          removed=$((removed + 1))
          ;;
        *)
          log SKIP "$dst points at $target; leaving it alone"
          ;;
      esac
    elif [ -d "$dst" ]; then
      log SKIP "$dst is a directory (plugin or manual install); remove it manually"
    fi
  done
  log INFO "Removed $removed skill link(s) from $SKILLS_DIR"
}

if [ "$UNINSTALL" = true ]; then
  uninstall_skills
else
  sync_checkout
  install_skills
fi
