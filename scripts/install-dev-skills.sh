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
#   CLAUDE_SKILLS_DIR        symlink target (default: ~/.claude/skills; set this
#                            or pass --target to install for another harness,
#                            for example Codex CLI's ~/.agents/skills)
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
                   (only links into the configured clone are removed)
  --force          replace a pre-existing skill entry (a directory, or a symlink
                   to another checkout) and discard uncommitted changes in an
                   existing clone
  --ref <ref>      branch, tag, or commit to check out (default: main)
  --dir <path>     shallow clone location (default: ~/.cache/eval-hub-skills);
                   with --uninstall it selects the clone whose links are removed
  --target <path>  harness skills directory (default: ~/.claude/skills; for
                   Codex CLI pass ~/.agents/skills)
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

# Collapse duplicate slashes and "." / ".." segments without requiring the path
# to exist. $CHECKOUT_DIR (from --dir / EVALHUB_SKILLS_DIR) and the targets
# stored in existing symlinks both pass through this, so the two compare equal
# even when a caller-supplied path contains "..".
canonical_path() {
  local part out=""
  local -a parts
  IFS=/ read -r -a parts <<<"$1"
  for part in "${parts[@]}"; do
    case "$part" in
      '' | .) ;;
      ..) out="${out%/*}" ;;
      *) out="$out/$part" ;;
    esac
  done
  printf '%s\n' "${out:-/}"
}

# Resolve a path to an absolute one without requiring it to exist yet. Symlink
# targets are stored verbatim, so a relative --dir / EVALHUB_SKILLS_DIR would
# otherwise be resolved against the skills directory instead of the CWD. A
# leading tilde is expanded here too: make passes variable values through
# quoted (--target "~/.agents/skills"), so the shell never expands it.
abspath() {
  case "$1" in
    "~") canonical_path "$HOME" ;;
    "~/"*) canonical_path "$HOME/${1#"~/"}" ;;
    /*) canonical_path "$1" ;;
    *) canonical_path "$PWD/$1" ;;
  esac
}

# Follow the target stored in a symlink. Relative targets are resolved against
# the directory holding the link and "." / ".." segments are collapsed, so the
# result compares equal to $CHECKOUT_DIR/dev-skills/<skill> even for a link that
# was created by hand instead of by this script.
resolved_link_target() {
  local link="$1" target
  target="$(readlink "$link" || true)"
  [ -n "$target" ] || return 0
  case "$target" in
    /*) ;;
    *) target="$(dirname "$link")/$target" ;;
  esac
  canonical_path "$target"
}

# Normalise a git remote URL (https://host/path.git, ssh://git@host/path,
# git@host:path) so that the same repository compares equal in every form.
normalize_remote_url() {
  local url="$1"
  url="${url%/}"
  url="${url%.git}"
  case "$url" in
    *://*) url="${url#*://}" ;;
    *:*) url="${url%%:*}/${url#*:}" ;;
  esac
  url="${url#*@}"
  printf '%s\n' "$url"
}

CHECKOUT_DIR="$(abspath "$CHECKOUT_DIR")"
SKILLS_DIR="$(abspath "$SKILLS_DIR")"

# Switching refs updates the working tree, so refuse to throw away local work
# unless --force explicitly authorises it.
ensure_checkout_clean() {
  local dirty
  dirty="$(git -C "$CHECKOUT_DIR" status --porcelain)"
  [ -z "$dirty" ] && return 0
  [ "$FORCE" = true ] && return 0
  fail "$CHECKOUT_DIR has uncommitted changes; commit or stash them, remove the directory, or rerun with --force to discard them"
}

# A --dir path can point at any clone, so confirm it belongs to REPO_URL before
# fetching through its origin or linking skills out of it.
verify_checkout_origin() {
  local origin
  origin="$(git -C "$CHECKOUT_DIR" config --get remote.origin.url || true)"
  [ -n "$origin" ] ||
    fail "$CHECKOUT_DIR is a git checkout without an 'origin' remote; remove it or pass a different --dir / EVALHUB_SKILLS_DIR"
  [ "$(normalize_remote_url "$origin")" = "$(normalize_remote_url "$REPO_URL")" ] ||
    fail "$CHECKOUT_DIR is a checkout of $origin, not $REPO_URL; remove it or pass a different --dir / EVALHUB_SKILLS_DIR"
}

fetch_ref() {
  ensure_checkout_clean
  if ! git -C "$CHECKOUT_DIR" fetch --depth 1 --quiet origin "$REF"; then
    fail "unable to fetch '$REF' from $REPO_URL; check --ref / EVALHUB_SKILLS_REF"
  fi
  if [ "$FORCE" = true ]; then
    git -C "$CHECKOUT_DIR" checkout --quiet --force FETCH_HEAD
  else
    git -C "$CHECKOUT_DIR" checkout --quiet FETCH_HEAD
  fi
}

sync_checkout() {
  if [ -e "$CHECKOUT_DIR" ] && [ ! -d "$CHECKOUT_DIR/.git" ]; then
    fail "$CHECKOUT_DIR exists but is not a git checkout; remove it or pass --dir"
  fi

  if [ -d "$CHECKOUT_DIR/.git" ]; then
    verify_checkout_origin
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
  local skill src dst current
  for skill in "${SKILLS[@]}"; do
    src="$CHECKOUT_DIR/dev-skills/$skill"
    dst="$SKILLS_DIR/$skill"
    if [ -L "$dst" ]; then
      # Only refresh a link this install owns. A link into another checkout, or
      # one created by hand, may belong to someone else, so leave it in place
      # unless --force explicitly authorises replacing it.
      current="$(resolved_link_target "$dst")"
      if [ "$FORCE" != true ] && [ "$current" != "$src" ]; then
        log SKIP "$dst points at ${current:-an unresolvable target}; rerun with --force to replace it"
        skipped=$((skipped + 1))
        continue
      fi
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
  local skill dst src current
  for skill in "${SKILLS[@]}"; do
    dst="$SKILLS_DIR/$skill"
    if [ -L "$dst" ]; then
      src="$CHECKOUT_DIR/dev-skills/$skill"
      current="$(resolved_link_target "$dst")"
      # Only remove a link this install owns, matching install_skills: a link
      # into another checkout, or one created by hand, may belong to someone
      # else. Point --dir / EVALHUB_SKILLS_DIR at the clone the link was
      # installed from to remove it.
      if [ "$current" = "$src" ]; then
        rm -f "$dst"
        log OK "removed $dst"
        removed=$((removed + 1))
      else
        log SKIP "$dst points at ${current:-an unresolvable target}, not $src; leaving it alone (pass --dir / EVALHUB_SKILLS_DIR matching the clone it was installed from)"
      fi
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
