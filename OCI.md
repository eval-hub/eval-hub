# OCI exports

OCI export is optional. Set `exports.oci` on an evaluation job to configure its
registry and repository. Jobs without this setting do not export evaluation cards
to OCI. OCI processing has its own status in `results.oci.status`; an export failure
does not replace the evaluation outcome or discard benchmark results.

For `completed` and `partially_failed` evaluations, the service publishes the
card and then a final bundle index containing every recorded completed benchmark
artifact. Failed and cancelled benchmark runs are excluded. OCI status `completed`
confirms that both published references were saved. A bundle failure retains the
published card and reports OCI status `failed`, preserving the evaluation outcome.
Signing and automatic export retries are not currently implemented.

## Artifacts and their relationships

| Output | Representation | Contents |
| --- | --- | --- |
| Evaluation card | OCI image manifest | A config blob and a JSON layer containing the job's evaluation card |
| Benchmark artifact | Independently published OCI image or Docker schema-2 manifest | Benchmark output files packaged by the adapter/SDK |
| Evaluation bundle | OCI image index | References to the evaluation card manifest and selected benchmark manifests |

The bundle is a registry object, not a tar archive or a file containing copies of
all results. Saving its JSON locally as `evaluation-bundle-index.json` gives you the
index document. Its `manifests` array references child manifests; each child's
`layers` array references that artifact's content blobs.

All children must be available in the job's configured registry and repository.
The bundle publisher retrieves children using their recorded digests and those
configured coordinates. It does not follow callback-supplied `oci_reference` URLs
or copy artifacts from other repositories.

## Job request

Submit a job with `POST /api/v1/evaluations/jobs`. This example shows a single
benchmark and OCI export to an authenticated registry; model and provider settings
must match your deployment.

```json
{
  "name": "oci-evaluation-example",
  "model": {
    "name": "my-model",
    "url": "https://model.example.com/v1"
  },
  "benchmarks": [
    {
      "id": "arc_easy",
      "provider_id": "lm_evaluation_harness",
      "parameters": { "num_examples": 5 }
    }
  ],
  "exports": {
    "oci": {
      "coordinates": {
        "oci_host": "quay.io",
        "oci_repository": "my-org/eval-results",
        "oci_tag": "release",
        "annotations": { "example.com/run-purpose": "release-validation" }
      },
      "k8s": { "connection": "eval-registry-credentials" }
    }
  }
}
```

In Kubernetes, `k8s.connection` names a `kubernetes.io/dockerconfigjson` Secret in
the tenant namespace, with a `.dockerconfigjson` entry containing credentials for
the registry. The service resolves those credentials and exchanges them for a
registry bearer token when authentication is required. Manifest reads and writes
use the same OCI client authentication path, including a single retry after a
401-triggered token refresh.

Local mode uses anonymous credentials and ignores Kubernetes connection settings.
For the local registry, omit `k8s` and set `oci_host` to `http://localhost:5001`.
An explicit `http://` enables plain HTTP; otherwise the client uses HTTPS. The
service's OCI HTTP client uses the configured `sidecar.oci` timeout and CA settings,
falling back to the cluster service CA or system roots.

`oci_tag` is a bundle tag **prefix**. With job ID `job-123`, the example produces
`release-job-123`; without a prefix, the bundle tag is `job-123`. A nonempty prefix
must match `^[a-zA-Z0-9_][a-zA-Z0-9._-]*$`. Long prefixes are truncated to fit the
128-character final tag limit while preserving the full job ID. The evaluation
card always uses `evaluation-card-job-123`, independently of this prefix.
Adapters receive no job-level tag override and use their existing fallback tags.
The full supplied prefix remains in the stored job configuration.

## Evaluation card manifest and annotations

The service publishes a card manifest with media type
`application/vnd.oci.image.manifest.v1+json`. Its JSON content layer uses
`application/vnd.eval-hub.evaluation-card.v1+json`; the config uses
`application/vnd.oci.artifact.config.v1+json` and contains the evaluation job ID.

Below and in subsequent examples, `job-123` is an illustrative job ID. Digests and
byte sizes are illustrative, not hashes or sizes computed from these snippets.
Actual descriptors contain the digest and byte length of the referenced content.

```json
{
  "schemaVersion": 2,
  "mediaType": "application/vnd.oci.image.manifest.v1+json",
  "config": {
    "mediaType": "application/vnd.oci.artifact.config.v1+json",
    "size": 31,
    "digest": "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
    "annotations": {
      "evaluation_job_id": "job-123",
      "org.opencontainers.image.title": "evaluation-card-job-123-config.json"
    }
  },
  "layers": [
    {
      "mediaType": "application/vnd.eval-hub.evaluation-card.v1+json",
      "size": 2048,
      "digest": "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
      "annotations": {
        "evaluation_job_id": "job-123",
        "org.opencontainers.image.title": "evaluation-card-job-123.json"
      }
    }
  ],
  "annotations": {
    "example.com/run-purpose": "release-validation",
    "evaluation_job_id": "job-123",
    "org.opencontainers.image.title": "evaluation-card-job-123"
  }
}
```

User annotations from `exports.oci.coordinates.annotations` are attached to the
card manifest. The service sets `evaluation_job_id` and
`org.opencontainers.image.title`, overriding user values for those keys. Config
and layer descriptor annotations are generated separately, as shown above.

## Benchmark OCI artifacts

Benchmark artifacts are published independently by the adapter/SDK. Their files,
layer media types, titles, and internal annotations depend on that publisher;
EvalHub's bundle publisher does not impose a new benchmark layer layout or rewrite
the benchmark manifest.

A recorded benchmark result identifies its published manifest through
`artifacts.oci_digest`. For example, this is the relevant part of a job response:

```json
{
  "results": {
    "benchmarks": [
      {
        "id": "arc_easy",
        "provider_id": "lm_evaluation_harness",
        "benchmark_index": 0,
        "artifacts": {
          "oci_digest": "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
          "oci_reference": "quay.io/my-org/eval-results@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
        }
      }
    ]
  }
}
```

Bundling requires a valid SHA-256 manifest digest for each selected completed
result. `oci_reference` is informational for this operation. The publisher accepts
OCI image manifests and Docker schema-2 single-image manifests, verifies their
bytes against the requested digest, and checks their descriptors. It does not
download the child blobs. Benchmark identity annotations are added to the
**index entry**, as shown below, without changing the benchmark artifact itself.

## Final evaluation bundle index and annotations

For a `completed` or `partially_failed` job, the publisher places the evaluation
card first, followed by recorded completed benchmark results in ascending
`benchmark_index` order. Failed and cancelled benchmark statuses contribute to
the skipped count. Selection uses recorded results and statuses, not a fresh
expansion of the requested collection or benchmark definitions. A card-only index
is possible when there are no selected benchmark results.

Every selected child must verify before the index is published. A missing or
invalid digest on a selected completed result causes publication to fail rather
than silently excluding that result.

This is a sample final index for one completed benchmark, published under
`quay.io/my-org/eval-results:release-job-123`:

```json
{
  "schemaVersion": 2,
  "mediaType": "application/vnd.oci.image.index.v1+json",
  "manifests": [
    {
      "mediaType": "application/vnd.oci.image.manifest.v1+json",
      "size": 1024,
      "digest": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
      "annotations": { "artifact_role": "evaluation-card" }
    },
    {
      "mediaType": "application/vnd.oci.image.manifest.v1+json",
      "size": 768,
      "digest": "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
      "annotations": {
        "artifact_role": "benchmark",
        "benchmark_index": "0",
        "benchmark_id": "arc_easy",
        "provider_id": "lm_evaluation_harness"
      }
    }
  ],
  "annotations": {
    "example.com/run-purpose": "release-validation",
    "evaluation_job_id": "job-123",
    "evaluation_state": "completed",
    "included_benchmark_count": "1",
    "skipped_benchmark_count": "0"
  }
}
```

Annotation placement matters:

| Location | Annotations | Meaning |
| --- | --- | --- |
| Bundle index | User annotations plus `evaluation_job_id`, `evaluation_state`, `included_benchmark_count`, `skipped_benchmark_count` | Job identity, outcome, and bundle membership counts |
| Card entry in `manifests` | `artifact_role: evaluation-card` | Identifies the card child |
| Benchmark entry in `manifests` | `artifact_role: benchmark`, `benchmark_index`, `benchmark_id`, `provider_id` | Identifies the benchmark run represented by this child |
| Card manifest, config, and layer | See the card example above | Card identity and human-readable names |
| Benchmark manifest and layers | Defined by its adapter/SDK | Original benchmark content metadata |

All annotation values are strings, including indices and counts. Generated bundle
annotations override user values for the four job-level keys. Child descriptor
annotations are created by the bundle publisher; user annotations are not copied
onto each child. Descriptor sizes refer to child **manifest bytes**, not the total
size of all artifact layers.

## Service export workflow

The terminal-transition handler invokes one shared export path for both runtime
modes. For OCI-enabled completed or partially failed evaluations, it persists
`running`, exports the card to configured destinations, then uses a per-job bundle
publisher to verify recorded completed benchmark manifests and publish the final
index. Independent destination failures do not block successful OCI work.

Card and bundle factories share credential resolution and HTTP/TLS configuration;
each resolves a client for the job's registry, repository, and tenant. Kubernetes
credentials come from a tenant Secret; local publication uses anonymous access.
The shared adapter job-spec builder clears the job-level tag on a coordinate copy,
reserving that prefix for the final index while adapters choose their fallback tags.

One final tenant-scoped transaction merges both published references with OCI
`completed`. Bundle failures save the available card with `failed`; a failed final
save attempts a fallback carrying both references. The evaluation outcome remains
independent. There is no intermediate card write or signing execution.

The unchanged-terminal-state guard prevents sequential duplicate callbacks from
repeating exports. It does not guarantee exactly-once execution under concurrent
callbacks. Registry publication and database persistence are separate: a crash
before final persistence can leave artifacts published with OCI status still
`running`. Automatic retries, recovery, and backfill remain future work.

## Job response: `results.oci`

Read the job using `GET /api/v1/evaluations/jobs/{id}`. OCI processing reports
`running`, then `completed` or `failed`, independently of the evaluation state.
Unavailable objects are omitted; a completed evaluation alone does not guarantee
an OCI reference exists.

The workflow returns this shape after successful card and bundle publication
(relevant response fields only):

```json
{
  "results": {
    "oci": {
      "status": {
        "state": "completed",
        "message": {
          "message": "OCI processing completed",
          "message_code": "oci_processing_completed"
        }
      },
      "evaluation_card": {
        "oci_digest": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
        "oci_reference": "quay.io/my-org/eval-results:evaluation-card-job-123@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
      },
      "evaluation_bundle": {
        "oci_digest": "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
        "oci_reference": "quay.io/my-org/eval-results:release-job-123@sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
      }
    }
  }
}
```

The bundle digest identifies the index JSON; the card digest identifies its child
manifest. Neither is the digest of the card's JSON layer. Use the returned
digest-qualified references to retrieve published outputs. `signing` remains
omitted until a signing workflow supplies an actual outcome; configuring signing
does not currently perform it.

## Quick local OCI card and bundle check

Developers can use `scripts/test-local-oci-export.sh` to check OCI publication
during development. It runs an evaluation with the local test adapter,
downloads the bundle index, every child's manifest and config blob, and the
evaluation-card JSON layer from a local OCI registry. It checks downloaded bytes
against their digests and sizes, and verifies membership, annotations, and saved
API results. Benchmark content layers are not downloaded.
No model server is required.

Check `results.oci.status.state` for the OCI export status: `running`, `completed`,
or `failed`. After a successful export, `results.oci.evaluation_card` provides the
manifest digest and digest-qualified reference for retrieving the card. An OCI
export failure leaves the evaluation's completed status and benchmark results intact.

Install these dependencies before starting:

- `curl`, `jq`, `oras`, and `shasum`, available on your `PATH`.
- Go (the version specified in `go.mod`) and `uv` for the service setup.
- A running container engine: Podman by default, or Docker with `DOCKER=docker`.

From the repository root, start the registry and service:

```sh
make start-oci-registry
make start-service
```

These targets start the registry at `localhost:5001` and build and start the
service in local mode at `http://localhost:8080`, including the test adapter setup.
For Docker, use `make start-oci-registry DOCKER=docker`.

Run the check:

```sh
bash scripts/test-local-oci-export.sh
OCI_TAG=custom OUTPUT_DIR=/tmp/local-oci-custom bash scripts/test-local-oci-export.sh
BENCHMARK_COUNT=3 OUTPUT_DIR=/tmp/local-oci-repeated bash scripts/test-local-oci-export.sh
BENCHMARK_COUNT=3 FAIL_BENCHMARK_INDEX=1 OUTPUT_DIR=/tmp/local-oci-partial bash scripts/test-local-oci-export.sh
COLLECTION_ID=coding-v1 OUTPUT_DIR=/tmp/local-oci-collection bash scripts/test-local-oci-export.sh
```

A successful run prints a verification message and saves these files in
`./bin/local-oci/` by default:

| File | Contents |
| --- | --- |
| `evaluation-job-request.json` | Submitted evaluation job request |
| `evaluation-job-create-response.json` | Job creation response, including the job ID |
| `evaluation-job-status-response.json` | Latest job status response, with results on completion |
| `evaluation-bundle-index.json` | Final OCI index |
| `evaluation-bundle-child-<index>-manifest.json` | Bundle child manifests fetched by digest; index 0 is the card, remaining indices are benchmark artifacts |
| `evaluation-bundle-child-<index>-config.json` | Config blob referenced by the corresponding child manifest |
| `evaluation-job-retrieved-response.json` | Subsequent job GET confirming saved OCI outputs |
| `evaluation-card-<job-id>.json` | Downloaded evaluation card |

Child indices are positions in the bundle, not benchmark indices. Child 0 is the
evaluation card; subsequent children are benchmark artifacts. The script downloads
and verifies each child's manifest and config blob. It also downloads the card's
JSON layer as `evaluation-card-<job-id>.json`; benchmark content layers are not
downloaded. The bundle index itself has no config blob or content layers.

For a bundle containing the card and one benchmark, the artifact downloads are:

```text
bin/local-oci/
  evaluation-bundle-index.json             # Bundle index document
  evaluation-bundle-child-0-manifest.json  # Evaluation-card manifest
  evaluation-bundle-child-0-config.json    # Evaluation-card config blob
  evaluation-card-<job-id>.json            # Evaluation-card JSON layer
  evaluation-bundle-child-1-manifest.json  # Benchmark manifest
  evaluation-bundle-child-1-config.json    # Benchmark config blob
```

The card manifest and config are saved only under their child filenames. The
script identifies the card using the descriptor's `artifact_role` annotation
after verifying that it occupies position 0 in the bundle.

Set `OUTPUT_DIR` to save verification files elsewhere:

```sh
OUTPUT_DIR=/tmp/local-oci-check bash scripts/test-local-oci-export.sh
```

When finished, stop the service and remove the registry container:

```sh
make stop-service
make stop-oci-registry
```

If using Docker, use `make stop-oci-registry DOCKER=docker`.
