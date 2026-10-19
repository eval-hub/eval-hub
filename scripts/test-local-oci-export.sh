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
oci_tag=${OCI_TAG:-}
collection_id=${COLLECTION_ID:-}
benchmark_count=${BENCHMARK_COUNT:-1}
fail_index=${FAIL_BENCHMARK_INDEX:--1}
expected_state=${EXPECTED_STATE:-completed}
if (( fail_index >= 0 )); then expected_state=partially_failed; fi

mkdir -p "$output_dir"

# Check the tools needed to submit the job and retrieve its exported artifact.
for tool in curl jq oras shasum; do
  command -v "$tool" >/dev/null || { printf 'Missing required tool: %s\n' "$tool" >&2; exit 1; }
done

# Build a job request with anonymous OCI export over plain HTTP.
# The local test adapter returns deterministic results without a model server.
jq -n \
  --arg host "http://$registry" \
  --arg repo "$repository" \
  --arg tag "$oci_tag" \
  --arg collection "$collection_id" \
  --argjson count "$benchmark_count" \
  --argjson fail_index "$fail_index" \
  '{
    name: "local-oci-export-check",
    model: {
      name: "test",
      url: "http://localhost:9999"
    },
    benchmarks: [range($count) as $index |
      {
        id: "arc_easy",
        provider_id: "lm_evaluation_harness",
        parameters: {
          num_examples: 5,
          test_fail: ($index == $fail_index)
        }
      }
    ],
    exports: {
      oci: {
        coordinates: {
          oci_host: $host,
          oci_repository: $repo,
          oci_tag: $tag,
          annotations: {
            "local.test": "true"
          }
        }
      }
    }
  } | if $collection != "" then del(.benchmarks) | .collection = {id: $collection} else . end' > "$output_dir/evaluation-job-request.json"

# Submit the job and extract its ID from the creation response.
response_file="$output_dir/evaluation-job-create-response.json"
curl --fail-with-body -sS \
  -H 'Content-Type: application/json' \
  -d @"$output_dir/evaluation-job-request.json" \
  "$base_url/api/v1/evaluations/jobs" > "$output_dir/evaluation-job-create-response.json"
response_file=""

job_id=$(jq -er '.resource.id' "$output_dir/evaluation-job-create-response.json")

# Poll until the evaluation completes, retaining the latest API response.
completed=false
for ((attempt = 0; attempt < 120; attempt++)); do
  response_file="$output_dir/evaluation-job-status-response.json"
  curl --fail-with-body -sS \
    "$base_url/api/v1/evaluations/jobs/$job_id" > "$output_dir/evaluation-job-status-response.json"
  response_file=""

  state=$(jq -r '.status.state' "$output_dir/evaluation-job-status-response.json")

  oci_state=$(jq -r '.results.oci.status.state // ""' "$output_dir/evaluation-job-status-response.json")

  if [[ "$state" == "$expected_state" && "$oci_state" == completed ]]; then
    completed=true
    break
  fi

  if [[ "$state" == failed || "$state" == cancelled || "$oci_state" == failed ]]; then
    cat "$output_dir/evaluation-job-status-response.json"
    exit 1
  fi

  sleep 1
done

if [[ "$completed" != true ]]; then
  printf 'Timed out waiting for evaluation %s and OCI completion\n' "$job_id" >&2
  cat "$output_dir/evaluation-job-status-response.json" >&2
  exit 1
fi
printf 'Job status GET response: %s/evaluation-job-status-response.json\n' "${output_dir%/}"

# Verify the card's dedicated tag independently of the bundle's child descriptors.
ref="$registry/$repository:evaluation-card-$job_id"

# Verify the API reports the actual manifest digest, independently of the card layer.
manifest_digest=$(oras manifest fetch --plain-http --descriptor "$ref" | jq -er '.digest')
jq -e --arg digest "$manifest_digest" --arg ref "$ref@$manifest_digest" --arg state "$expected_state" '
  .status.state == $state and
  .results.oci.status.state == "completed" and
  (.results.oci.status.message.message | length > 0) and
  (.results.oci.status.message.message_code | length > 0) and
  .results.oci.evaluation_card.oci_digest == $digest and
  .results.oci.evaluation_card.oci_reference == $ref and
  (.results.oci.evaluation_bundle.oci_digest | startswith("sha256:")) and
  (.results.oci.evaluation_bundle.oci_reference | length > 0) and .results.oci.signing == null
' "$output_dir/evaluation-job-status-response.json" >/dev/null

# Retrieve the index by the API's saved digest and independently verify the tag.
allowance=$((128 - 1 - ${#job_id}))
bundle_tag="$job_id"
if [[ -n "$oci_tag" ]]; then bundle_tag="${oci_tag:0:allowance}-$job_id"; fi
bundle_ref="$registry/$repository:$bundle_tag"
bundle_digest=$(jq -er '.results.oci.evaluation_bundle.oci_digest' "$output_dir/evaluation-job-status-response.json")
jq -e --arg ref "$bundle_ref@$bundle_digest" \
  '.results.oci.evaluation_bundle.oci_reference == $ref' "$output_dir/evaluation-job-status-response.json" >/dev/null
oras manifest fetch --plain-http "$registry/$repository@$bundle_digest" > "$output_dir/evaluation-bundle-index.json"
[[ "$bundle_digest" == "$(oras manifest fetch --plain-http --descriptor "$bundle_ref" | jq -er '.digest')" ]]
[[ "$bundle_digest" == "sha256:$(shasum -a 256 "$output_dir/evaluation-bundle-index.json" | cut -d ' ' -f 1)" ]]

# Derive membership from recorded results/statuses, preserving repeated runs and indices.
jq -e --arg id "$job_id" --arg card "$manifest_digest" --arg state "$expected_state" \
  --slurpfile job "$output_dir/evaluation-job-status-response.json" '
  ($job[0].status.benchmarks | map(select(.status == "completed") | .benchmark_index)) as $completed |
  ($job[0].results.benchmarks | map(select(.benchmark_index as $i | $completed | index($i))) | sort_by(.benchmark_index)) as $included |
  ($job[0].status.benchmarks | map(select(.status == "failed" or .status == "cancelled")) | length) as $skipped |
  .mediaType == "application/vnd.oci.image.index.v1+json" and
  .annotations.evaluation_job_id == $id and .annotations.evaluation_state == $state and
  .annotations["local.test"] == "true" and
  .annotations.included_benchmark_count == ($included | length | tostring) and
  .annotations.skipped_benchmark_count == ($skipped | tostring) and
  (.manifests | length) == (($included | length) + 1) and
  .manifests[0].digest == $card and .manifests[0].annotations.artifact_role == "evaluation-card" and
  ([.manifests[1:][] | {digest, index: .annotations.benchmark_index, id: .annotations.benchmark_id, provider: .annotations.provider_id, role: .annotations.artifact_role}] ==
   [$included[] | {digest: .artifacts.oci_digest, index: (.benchmark_index | tostring), id, provider: .provider_id, role: "benchmark"}])
' "$output_dir/evaluation-bundle-index.json" >/dev/null

# Fetch every child's manifest and config, validating their exact bytes.
# Only the evaluation-card child needs its content layer downloaded.
child_index=0
card="${output_dir%/}/evaluation-card-$job_id.json"
card_manifest=""
card_config=""
while IFS= read -r descriptor; do
  child_digest=$(jq -er '.digest' <<< "$descriptor")
  child_file="$output_dir/evaluation-bundle-child-$child_index-manifest.json"
  config_file="$output_dir/evaluation-bundle-child-$child_index-config.json"
  oras manifest fetch --plain-http "$registry/$repository@$child_digest" > "$child_file"
  [[ "$child_digest" == "sha256:$(shasum -a 256 "$child_file" | cut -d ' ' -f 1)" ]]
  child_size=$(wc -c < "$child_file" | tr -d ' ')
  child_media_type=$(jq -er '.mediaType' "$child_file")
  jq -e --argjson size "$child_size" --arg media "$child_media_type" \
    '.size == $size and .mediaType == $media' <<< "$descriptor" >/dev/null

  config_digest=$(jq -er '.config.digest' "$child_file")
  oras blob fetch --plain-http "$registry/$repository@$config_digest" --output "$config_file"
  [[ "$config_digest" == "sha256:$(shasum -a 256 "$config_file" | cut -d ' ' -f 1)" ]]
  config_size=$(wc -c < "$config_file" | tr -d ' ')
  jq -e --argjson size "$config_size" '.config.size == $size' "$child_file" >/dev/null

  if [[ "$(jq -er '.annotations.artifact_role' <<< "$descriptor")" == evaluation-card ]]; then
    card_manifest="$child_file"
    card_config="$config_file"
    layer_digest=$(jq -er '.layers[0].digest' "$child_file")
    [[ "$manifest_digest" != "$layer_digest" ]]
    oras blob fetch --plain-http "$registry/$repository@$layer_digest" --output "$card"
    [[ "$layer_digest" == "sha256:$(shasum -a 256 "$card" | cut -d ' ' -f 1)" ]]
    layer_size=$(wc -c < "$card" | tr -d ' ')
    jq -e --argjson size "$layer_size" '.layers[0].size == $size' "$child_file" >/dev/null
    printf 'Evaluation card downloaded by ORAS: %s\n' "$card"
  fi
  child_index=$((child_index + 1))
done < <(jq -c '.manifests[]' "$output_dir/evaluation-bundle-index.json")

# Verify that the evaluation-card OCI config belongs to the submitted job.
jq -e \
  --arg id "$job_id" \
  '.evaluation_job_id == $id' \
  "$card_config" >/dev/null

# Verify job association, the requested annotation, and the single card layer.
jq -e \
  --arg id "$job_id" \
  '
    .annotations.evaluation_job_id == $id and
    .annotations["local.test"] == "true" and
    (.layers | length) == 1
  ' \
  "$card_manifest" >/dev/null

# Compare the retrieved card with the completed job returned by the API.
jq -e \
  --arg id "$job_id" \
  --arg state "$expected_state" \
  --slurpfile job "$output_dir/evaluation-job-status-response.json" \
  '
    .metadata.evaluation_job_id == $id and
    .results.status.state == $state and
    ([.results.benchmarks[] | {id, provider_id, metrics}] | sort) ==
    ([$job[0].results.benchmarks[] | {id, provider_id, metrics}] | sort)
  ' \
  "$card" >/dev/null

# A subsequent GET must retain the saved references and evaluation outcome.
curl --fail-with-body -sS "$base_url/api/v1/evaluations/jobs/$job_id" > "$output_dir/evaluation-job-retrieved-response.json"
jq -e --slurpfile original "$output_dir/evaluation-job-status-response.json" \
  '.results.oci == $original[0].results.oci and .status.state == $original[0].status.state' "$output_dir/evaluation-job-retrieved-response.json" >/dev/null
printf 'Verified OCI evaluation card and bundle for job %s in %s\n' "$job_id" "$output_dir"
