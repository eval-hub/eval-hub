#!/usr/bin/env bash

# Go-style duration arithmetic for the Makefile.
#
# The benchmark knobs (BENCHMARK_TIMEOUT, KIND_BENCHMARK_TIMEOUT) and
# `kubectl create token --duration` take Go durations such as "90m" or "1h30m",
# but the shell cannot add or compare them as numbers. The kind benchmark target
# needs both: it mints one ServiceAccount token for the whole run, so the token
# lifetime is derived from the suite deadline plus a margin, and a deadline that
# outlives the token is rejected before anything runs.
#
#   scripts/go-duration.sh seconds <duration>    print the duration in seconds
#   scripts/go-duration.sh add <duration>...     print the sum as a Go duration
#   scripts/go-duration.sh at-least <a> <b>      exit 0 when a is not shorter than b
set -Eeuo pipefail

fail() {
  printf '[FAIL] %s\n' "$1" >&2
  exit 1
}

usage() {
  cat >&2 <<'USAGE'
Usage: scripts/go-duration.sh <command> [duration...]

  seconds <duration>   print the duration as whole seconds (90m -> 5400)
  add <duration>...    print the sum of the durations (4h 1h -> 5h)
  at-least <a> <b>     exit 0 when duration a is not shorter than duration b

Durations use Go syntax: 45s, 90m, 4h, 1h30m.
USAGE
  exit 2
}

# duration_seconds prints a Go duration as whole seconds. It rejects anything
# that is not a run of duration components, so a typo cannot silently become 0.
duration_seconds() {
  awk -v duration="$1" '
    BEGIN {
      if (duration == "") exit 1
      rest = duration
      seconds = 0
      while (match(rest, /[0-9]+(\.[0-9]+)?(ms|s|m|h)/)) {
        component = substr(rest, RSTART, RLENGTH)
        value = component + 0
        if (component ~ /ms$/) seconds += value / 1000
        else if (component ~ /h$/) seconds += value * 3600
        else if (component ~ /m$/) seconds += value * 60
        else seconds += value
        rest = substr(rest, 1, RSTART - 1) substr(rest, RSTART + RLENGTH)
      }
      if (rest != "") exit 1
      printf "%d\n", seconds + 0.5
    }'
}

# format_duration prints whole seconds as the shortest Go duration that parses
# back to the same value (3600 -> 1h, 5400 -> 1h30m, 45 -> 45s).
format_duration() {
  awk -v seconds="$1" '
    BEGIN {
      hours = int(seconds / 3600)
      minutes = int((seconds - hours * 3600) / 60)
      remainder = seconds - hours * 3600 - minutes * 60
      duration = ""
      if (hours > 0) duration = duration hours "h"
      if (minutes > 0) duration = duration minutes "m"
      if (remainder > 0 || duration == "") duration = duration remainder "s"
      print duration
    }'
}

case "${1:-}" in
  seconds)
    [[ $# -eq 2 ]] || usage
    seconds="$(duration_seconds "$2")" || fail "not a Go duration: '$2'"
    printf '%s\n' "$seconds"
    ;;
  add)
    shift
    [[ $# -ge 1 ]] || usage
    total=0
    for duration in "$@"; do
      seconds="$(duration_seconds "$duration")" || fail "not a Go duration: '$duration'"
      total=$((total + seconds))
    done
    format_duration "$total"
    ;;
  at-least)
    [[ $# -eq 3 ]] || usage
    actual="$(duration_seconds "$2")" || fail "not a Go duration: '$2'"
    minimum="$(duration_seconds "$3")" || fail "not a Go duration: '$3'"
    ((actual >= minimum))
    ;;
  *)
    usage
    ;;
esac
