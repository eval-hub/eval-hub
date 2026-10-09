#!/usr/bin/env bash
# Requires a running --local service using the repository's local test adapter.

set -euo pipefail

# Report the original failure before exiting, including API response details.
response_file=""

report_error() {
  local exit_code="$1"
  local line_number="$2"
  local command="$3"

  printf '\nERROR: %s:%s (exit code %s)\n' \
    "${BASH_SOURCE[0]}" "$line_number" "$exit_code" >&2
  printf 'Command: %s\n' "$command" >&2
  printf 'Evidence directory: %s\n' "${output_dir:-not configured}" >&2

  if [[ -n "$response_file" && -s "$response_file" ]]; then
    printf 'API response (%s):\n' "$response_file" >&2
    cat "$response_file" >&2
    printf '\n' >&2
  fi

  exit "$exit_code"
}

trap 'report_error "$?" "$LINENO" "$BASH_COMMAND"' ERR

# Configure the service, registry, and directory for retained test evidence.
base_url=${EVALHUB_BASE_URL:-http://localhost:8080}
registry=${OCI_REGISTRY:-localhost:5001}
repository=${OCI_REPOSITORY:-myorg/eval-results}
output_dir=${OUTPUT_DIR:-./bin/local-oci/}

mkdir -p "$output_dir"

# Check the tools needed to submit the job and retrieve its exported artifact.
for tool in curl jq oras; do
  command -v "$tool" >/dev/null || { printf 'Missing required tool: %s\n' "$tool" >&2; exit 1; }
done

# Build a job request with anonymous OCI export over plain HTTP.
# The local test adapter returns deterministic results without a model server.
jq -n \
  --arg host "http://$registry" \
  --arg repo "$repository" \
  '{
    name: "local-oci-export-check",
    model: {
      name: "test",
      url: "http://localhost:9999"
    },
    benchmarks: [
      {
        id: "arc_easy",
        provider_id: "lm_evaluation_harness",
        parameters: {
          num_examples: 5
        }
      }
    ],
    exports: {
      oci: {
        coordinates: {
          oci_host: $host,
          oci_repository: $repo,
          annotations: {
            "local.test": "true"
          }
        }
      }
    }
  }' > "$output_dir/request.json"

# Submit the job and extract its ID from the creation response.
response_file="$output_dir/created.json"
curl --fail-with-body -sS \
  -H 'Content-Type: application/json' \
  -d @"$output_dir/request.json" \
  "$base_url/api/v1/evaluations/jobs" > "$output_dir/created.json"
response_file=""

job_id=$(jq -er '.resource.id' "$output_dir/created.json")

# Poll until the evaluation completes, retaining the latest API response.
completed=false
for ((attempt = 0; attempt < 120; attempt++)); do
  response_file="$output_dir/job.json"
  curl --fail-with-body -sS \
    "$base_url/api/v1/evaluations/jobs/$job_id" > "$output_dir/job.json"
  response_file=""

  state=$(jq -r '.status.state' "$output_dir/job.json")

  if [[ "$state" == completed ]]; then
    completed=true
    break
  fi

  if [[ "$state" == failed || "$state" == cancelled ]]; then
    cat "$output_dir/job.json"
    exit 1
  fi

  sleep 1
done

[[ "$completed" == true ]]
printf 'Job status GET response: %s/job.json\n' "${output_dir%/}"

# EvalHub publishes the card under this job-specific tag. Export can finish
# after the evaluation, so poll the registry separately for its manifest.
ref="$registry/$repository:evaluation-card-$job_id"
exported=false
for ((attempt = 0; attempt < 60; attempt++)); do
  if oras manifest fetch --plain-http "$ref" \
    > "$output_dir/manifest.json" 2>/dev/null; then
    exported=true
    break
  fi

  sleep 1
done

[[ "$exported" == true ]]

# Pull the evaluation card layer and fetch the separate OCI config blob.
oras pull --plain-http "$ref" -o "$output_dir"
card="${output_dir%/}/evaluation-card-$job_id.json"
printf 'Evaluation card downloaded by ORAS: %s\n' "$card"

config_digest=$(jq -er '.config.digest' "$output_dir/manifest.json")
oras blob fetch --plain-http \
  "$registry/$repository@$config_digest" \
  --output "$output_dir/artifact-config.json"

# Verify that the OCI config belongs to the submitted job.
jq -e \
  --arg id "$job_id" \
  '.evaluation_job_id == $id' \
  "$output_dir/artifact-config.json" >/dev/null

# Verify job association, the requested annotation, and the single card layer.
jq -e \
  --arg id "$job_id" \
  '
    .annotations.evaluation_job_id == $id and
    .annotations["local.test"] == "true" and
    (.layers | length) == 1
  ' \
  "$output_dir/manifest.json" >/dev/null

# Compare the retrieved card with the completed job returned by the API.
jq -e \
  --arg id "$job_id" \
  --slurpfile job "$output_dir/job.json" \
  '
    .metadata.evaluation_job_id == $id and
    .results.status.state == "completed" and
    .results.benchmarks[0].metrics == $job[0].results.benchmarks[0].metrics
  ' \
  "$card" >/dev/null

printf 'Verified service-generated OCI evaluation card for job %s in %s\n' \
  "$job_id" "$output_dir"
