#!/usr/bin/env bash

# Container runtime selection shared by the dev scripts.
#
# dev-deploy records the engine that backs KIND_CLUSTER_NAME next to the private
# kubeconfig, and every dev script resolves the runtime from that record instead
# of probing both engines. resolve_runtime_decision decides which engine a script
# should use and the evidence it rests on, in one place; each script's
# detect_container_runtime is a policy table over that evidence, because the same
# fact calls for a different action depending on what the script is about to do.
# The remaining helpers query kind with the matching provider and read or write
# the record, so the four scripts share one implementation instead of four
# near-copies.
#
# Sourced by scripts/dev-deploy.sh, scripts/dev-clean.sh, scripts/dev-demo.sh and
# scripts/clean-container-storage.sh. The caller sets:
#
#   KIND_CLUSTER_NAME   cluster whose engine is resolved
#   RUNTIME_STATE_FILE  file holding that engine (see recorded_runtime)
#   CONTAINER_RUNTIME   explicit engine from the environment, or empty
#
# and provides log() and fail(), which the helpers use.

# kind_cmd runs kind with the provider the selected runtime needs.
kind_cmd() {
  if [[ "${CONTAINER_RUNTIME:-}" == podman ]]; then
    KIND_EXPERIMENTAL_PROVIDER=podman kind "$@"
  else
    env -u KIND_EXPERIMENTAL_PROVIDER kind "$@"
  fi
}

# canonical_runtime resolves a runtime name to the engine that actually runs it.
# The podman-docker package installs a `docker` wrapper that execs podman, so both
# names reach one engine and that wrapper reports itself through --version. The
# kind provider environment and the registry push flags depend on knowing it is
# really podman: treating the shim as docker would run kind with the docker
# provider and push to the plain-HTTP local registry without --tls-verify=false.
canonical_runtime() {
  local runtime="$1"
  if [[ "$runtime" == docker ]] && command -v docker >/dev/null 2>&1 &&
    docker --version 2>/dev/null | grep -qi podman; then
    printf 'podman\n'
  else
    printf '%s\n' "$runtime"
  fi
}

# kind_cluster_in_runtime returns 0 when KIND_CLUSTER_NAME exists under the given
# container runtime, using the kind provider that runtime requires. A runtime that
# is not installed or not reachable cannot own a cluster, so it reports false.
kind_cluster_in_runtime() {
  local runtime="$1" clusters
  command -v kind >/dev/null 2>&1 || return 1
  command -v "$runtime" >/dev/null 2>&1 || return 1
  "$runtime" info >/dev/null 2>&1 || return 1
  case "$runtime" in
    podman)
      clusters="$(KIND_EXPERIMENTAL_PROVIDER=podman kind get clusters 2>/dev/null || true)"
      ;;
    docker)
      clusters="$(env -u KIND_EXPERIMENTAL_PROVIDER kind get clusters 2>/dev/null || true)"
      ;;
    *)
      return 1
      ;;
  esac
  grep -Fqx -- "$KIND_CLUSTER_NAME" <<<"$clusters"
}

# recorded_runtime prints the engine dev-deploy recorded for KIND_CLUSTER_NAME, if
# any. The record keeps a redeploy, dev-clean, dev-demo, and
# clean-container-storage on the engine that backs the cluster without probing
# both engines for it.
recorded_runtime() {
  local recorded
  [[ -f "$RUNTIME_STATE_FILE" ]] || return 1
  recorded="$(head -n 1 "$RUNTIME_STATE_FILE" 2>/dev/null | tr -d '[:space:]')"
  case "$recorded" in
    podman|docker) printf '%s\n' "$recorded" ;;
    *) return 1 ;;
  esac
}

# record_runtime stores the engine that backs KIND_CLUSTER_NAME. Call it once the
# cluster exists, so a failed deploy does not claim an engine for a cluster it
# never created.
record_runtime() {
  local tmp
  # Write through a private temporary file and rename it into place, so a reader
  # never sees a partial record and a pre-placed symlink at a predictable path
  # cannot redirect the write. mktemp creates the file in the record's own
  # directory (same filesystem, so the rename stays atomic) with mode 600.
  tmp="$(mktemp "${RUNTIME_STATE_FILE}.tmp.XXXXXX")" ||
    fail "Could not create a temporary file for $RUNTIME_STATE_FILE."
  # Keep the record private whatever the caller's umask is, and remove the
  # temporary file if writing or renaming it fails.
  if ! printf '%s\n' "$CONTAINER_RUNTIME" >"$tmp" ||
    ! chmod 600 "$tmp" ||
    ! mv -f -- "$tmp" "$RUNTIME_STATE_FILE"; then
    rm -f -- "$tmp"
    fail "Could not record $CONTAINER_RUNTIME in $RUNTIME_STATE_FILE."
  fi
  log INFO "Recorded $CONTAINER_RUNTIME as the container runtime of kind cluster '$KIND_CLUSTER_NAME' in $RUNTIME_STATE_FILE."
}

# runtime_is_installed returns 0 when the CLI of the given runtime is installed. A
# record naming an engine that is no longer installed cannot describe a live
# cluster, so a caller ignores or drops that record instead of repeating a failure
# the user can only clear by editing the state file.
runtime_is_installed() {
  local engine
  engine="$(canonical_runtime "$1")"
  command -v "$engine" >/dev/null 2>&1
}

# runtime_is_reachable returns 0 when the given runtime is installed and answers.
# Unlike use_runtime it never exits, so a caller can fall back to another engine
# when the recorded one is stopped, uninstalled, or belongs to an unmounted podman
# machine.
runtime_is_reachable() {
  local engine
  engine="$(canonical_runtime "$1")"
  runtime_is_installed "$engine" || return 1
  "$engine" info >/dev/null 2>&1
}

# fallback_runtime_order prints the engine preference used when neither the record
# nor an existing cluster decides, one name per line: podman before docker, except
# under WSL where Docker is preferred, because WSL2 podman answers 'podman info'
# but kind node startup fails during systemd/cgroup setup.
fallback_runtime_order() {
  if uname -r 2>/dev/null | grep -qiE 'microsoft|wsl'; then
    printf 'docker\npodman\n'
  else
    printf 'podman\ndocker\n'
  fi
}

# runtime_other prints the container engine that is not $1, and nothing for any
# other value.
runtime_other() {
  case "$1" in
    podman) printf 'docker\n' ;;
    docker) printf 'podman\n' ;;
  esac
}

# first_reachable_runtime prints the first engine in fallback_runtime_order that
# is installed and answers, and fails when neither does. It is the last resort of
# a command that needs a working engine (a deploy, a cleanup, a prune).
first_reachable_runtime() {
  local runtime
  for runtime in $(fallback_runtime_order); do
    if runtime_is_reachable "$runtime"; then
      printf '%s\n' "$runtime"
      return 0
    fi
  done
  return 1
}

# cluster_owner prints the engine that runs KIND_CLUSTER_NAME, probing the engines
# in the given order, and fails when neither runs it. $1 is "fallback" (the
# default: fallback_runtime_order) or "query" (docker before podman, the order
# dev-demo probes with to report a missing cluster).
cluster_owner() {
  local runtime
  # Splitting the helper's newline-separated output is intended.
  if [[ "${1:-fallback}" == query ]]; then
    set -- docker podman
  else
    set -- $(fallback_runtime_order)
  fi
  for runtime in "$@"; do
    runtime="$(canonical_runtime "$runtime")"
    if kind_cluster_in_runtime "$runtime"; then
      printf '%s\n' "$runtime"
      return 0
    fi
  done
  return 1
}

# resolve_runtime_decision [fallback|query|prune] resolves which engine a dev
# script should use and the evidence it rests on, without logging, exiting, or
# acting. That decision is written once here; each script's
# detect_container_runtime is a policy table over the evidence, because the same
# fact means "fail before you create a duplicate cluster" to a deploy, "fall back
# to the owner" to a query, and "prune the engine the caller named" to a storage
# cleanup.
#
# $1 is what the caller needs to know. "fallback" (the default) probes for the
# engine that runs the cluster, and "query" asks docker before podman (the order
# dev-demo probes with to report a missing cluster). "prune" is for a storage
# cleanup, which cannot duplicate or destroy a cluster, so an explicit engine is
# taken as given and no engine is probed for ownership: an engine that stops
# answering (for example a wedged Docker Desktop) must not hold up reclaiming
# space. Prune mode therefore asks only whether the engine it was given answers
# and whether the recorded one is installed, never which cluster either runs.
#
# The caller sets KIND_CLUSTER_NAME, RUNTIME_STATE_FILE and (optionally)
# CONTAINER_RUNTIME, and reads back:
#
#   RESOLVED_RUNTIME       canonical engine for the decision, or empty
#   RESOLVED_EVIDENCE      one term from the table below
#   RESOLVED_OWNER         engine that runs the cluster when a probe found it
#   RESOLVED_RECORDED      canonical engine named by the record, if any
#   RESOLVED_RECORD_STATE  none | uninstalled | stopped | owned | stale | gone,
#                          or unchecked in prune mode, where an installed
#                          recorded engine is not probed
#
# Explicit CONTAINER_RUNTIME:
#   explicit-invalid             not podman or docker
#   explicit-missing             that engine is not installed or not reachable
#   explicit-requested           prune mode: it is installed and reachable, so it
#                                is used as given, with no ownership probe of
#                                either engine
#   explicit-owned               it runs the cluster
#   explicit-no-owner            nothing runs the cluster; it is used
#   explicit-other-owns          no usable record; the other engine runs it
#   explicit-recorded-stale      it matches a record whose engine no longer runs
#                                the cluster; the other engine does
#   explicit-recorded-unverified it matches a record whose engine is stopped, so
#                                the record cannot be checked; RESOLVED_OWNER is
#                                set when the other engine runs the cluster
#   explicit-mismatch            it differs from a trusted record (owned/stopped)
#   explicit-mismatch-stale      it differs from a record that no longer runs the
#                                cluster, while the other engine does
#   explicit-mismatch-gone       it differs from a record that runs nothing
# No explicit value:
#   recorded-owned               the recorded engine runs the cluster
#   recorded-stale               the record is reachable but the other engine runs
#                                the cluster: the record is stale
#   recorded-gone                the recorded engine runs no cluster of that name
#   recorded-stopped             the recorded engine is installed but not
#                                reachable, so the record cannot be checked;
#                                RESOLVED_OWNER is set when the other engine runs
#                                the cluster
#   owner                        no usable record; this engine runs the cluster
#   no-owner                     nothing runs the cluster (or could be probed)
#
# A record whose engine is installed and runs the cluster answers the ownership
# question, so the other engine is not probed: that is the point of the record.
# The other engine is probed only to tell a stale record from a gone cluster, to
# name the engine that runs the cluster while the recorded one is stopped, or
# when no usable record exists.
resolve_runtime_decision() {
  local order="${1:-fallback}" explicit="" recorded="" other="" record_state=none
  RESOLVED_RUNTIME=""
  RESOLVED_EVIDENCE=""
  RESOLVED_OWNER=""
  RESOLVED_RECORDED=""
  RESOLVED_RECORD_STATE=none

  if [[ -n "${CONTAINER_RUNTIME:-}" ]]; then
    case "$CONTAINER_RUNTIME" in
      podman|docker) explicit="$(canonical_runtime "$CONTAINER_RUNTIME")" ;;
      *)
        RESOLVED_EVIDENCE=explicit-invalid
        return 0
        ;;
    esac
  fi

  if [[ -n "$explicit" && "$order" == prune ]]; then
    # A storage cleanup reclaims space from the engine it was given and cannot
    # duplicate or destroy a cluster, so ownership corroboration would buy it
    # nothing: the other engine -- possibly the one that never answers -- is not
    # probed, and neither is kind. The record is still read, because its engine
    # name is worth a warning when the caller asked for another one.
    RESOLVED_RUNTIME="$explicit"
    if recorded="$(recorded_runtime)"; then
      recorded="$(canonical_runtime "$recorded")"
      RESOLVED_RECORDED="$recorded"
      if runtime_is_installed "$recorded"; then
        RESOLVED_RECORD_STATE=unchecked
      else
        RESOLVED_RECORD_STATE=uninstalled
      fi
    fi
    if runtime_is_reachable "$explicit"; then
      RESOLVED_EVIDENCE=explicit-requested
    else
      RESOLVED_EVIDENCE=explicit-missing
    fi
    return 0
  fi

  if recorded="$(recorded_runtime)"; then
    recorded="$(canonical_runtime "$recorded")"
    RESOLVED_RECORDED="$recorded"
    if ! runtime_is_installed "$recorded"; then
      # An uninstalled engine cannot describe a reachable cluster, so the record
      # is ignored; callers may drop it so it cannot reject a later command.
      record_state=uninstalled
      recorded=""
    elif ! runtime_is_reachable "$recorded"; then
      # The recorded engine cannot be asked, so the record itself cannot be
      # checked -- but the other engine can still report that it runs the
      # cluster, which a deploy has to name and a prune has to choose.
      record_state=stopped
      other="$(runtime_other "$recorded")"
      if kind_cluster_in_runtime "$other"; then
        RESOLVED_OWNER="$other"
      fi
    elif kind_cluster_in_runtime "$recorded"; then
      record_state=owned
    else
      # The record's engine answers but does not run the cluster: ask the other
      # engine, the only way to tell a stale record from a gone cluster.
      other="$(runtime_other "$recorded")"
      if kind_cluster_in_runtime "$other"; then
        record_state=stale
      else
        record_state=gone
      fi
    fi
  fi
  RESOLVED_RECORD_STATE="$record_state"

  if [[ -n "$explicit" ]]; then
    RESOLVED_RUNTIME="$explicit"
    if ! runtime_is_installed "$explicit"; then
      RESOLVED_EVIDENCE=explicit-missing
      return 0
    fi
    case "$record_state" in
      owned|stopped)
        if [[ "$recorded" != "$explicit" ]]; then
          RESOLVED_EVIDENCE=explicit-mismatch
        elif [[ "$record_state" == owned ]]; then
          RESOLVED_EVIDENCE=explicit-owned
          RESOLVED_OWNER="$explicit"
        else
          RESOLVED_EVIDENCE=explicit-recorded-unverified
        fi
        ;;
      stale)
        RESOLVED_OWNER="$other"
        if [[ "$recorded" != "$explicit" ]]; then
          RESOLVED_EVIDENCE=explicit-mismatch-stale
        else
          RESOLVED_EVIDENCE=explicit-recorded-stale
        fi
        ;;
      gone)
        if [[ "$recorded" != "$explicit" ]]; then
          RESOLVED_EVIDENCE=explicit-mismatch-gone
        else
          RESOLVED_EVIDENCE=explicit-no-owner
        fi
        ;;
      *)
        # No usable record: ask both engines which one runs the cluster.
        if ! runtime_is_reachable "$explicit"; then
          RESOLVED_EVIDENCE=explicit-missing
        elif kind_cluster_in_runtime "$explicit"; then
          RESOLVED_EVIDENCE=explicit-owned
          RESOLVED_OWNER="$explicit"
        else
          other="$(runtime_other "$explicit")"
          if kind_cluster_in_runtime "$other"; then
            RESOLVED_EVIDENCE=explicit-other-owns
            RESOLVED_OWNER="$other"
          else
            RESOLVED_EVIDENCE=explicit-no-owner
          fi
        fi
        ;;
    esac
    return 0
  fi

  case "$record_state" in
    owned)
      RESOLVED_RUNTIME="$recorded"
      RESOLVED_EVIDENCE=recorded-owned
      RESOLVED_OWNER="$recorded"
      return 0
      ;;
    stale)
      RESOLVED_RUNTIME="$recorded"
      RESOLVED_EVIDENCE=recorded-stale
      RESOLVED_OWNER="$other"
      return 0
      ;;
    gone)
      RESOLVED_RUNTIME="$recorded"
      RESOLVED_EVIDENCE=recorded-gone
      return 0
      ;;
    stopped)
      RESOLVED_RUNTIME="$recorded"
      RESOLVED_EVIDENCE=recorded-stopped
      return 0
      ;;
  esac

  if RESOLVED_RUNTIME="$(cluster_owner "$order")"; then
    RESOLVED_EVIDENCE=owner
    RESOLVED_OWNER="$RESOLVED_RUNTIME"
  else
    RESOLVED_RUNTIME=""
    RESOLVED_EVIDENCE=no-owner
  fi
  return 0
}

# use_runtime makes ENGINE the selected runtime after checking that it exists and
# is reachable. The optional second argument names what the runtime is used for,
# so the log line matches the script ("for builds, the local registry, and kind").
use_runtime() {
  local engine scope="${2:-}"
  engine="$(canonical_runtime "$1")"
  command -v "$engine" >/dev/null 2>&1 || fail "Required command not found: $engine"
  "$engine" info >/dev/null 2>&1 ||
    fail "$engine is installed but not reachable. Start the $engine daemon (or podman machine) and retry."
  CONTAINER_RUNTIME="$engine"
  if [[ -n "$scope" ]]; then
    log INFO "Using $CONTAINER_RUNTIME as the container runtime $scope."
  else
    log INFO "Using $CONTAINER_RUNTIME as the container runtime."
  fi
}
