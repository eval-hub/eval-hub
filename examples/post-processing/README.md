# Standalone post-processing

## Data preparation in Kubernetes

Post-processing reuses the same `/app/eval-runtime-init` executable and configured
init image as evaluation jobs. For S3, Git and Hugging Face references, eval-hub
mounts the existing `job.json` into one container named `init`, which runs
before the computation adapter. Init reads the results and calibration references
from `parameters.operations.confidence_interval` and calls the existing downloaders
directly, using separate configuration and destinations for each input. Source credentials are mounted
only in init, while the adapter receives read-only staged data.

PVC references use direct read-only mounts with Kubernetes applying `sub_path`.
References sharing a claim share its volume but retain independent mounts.

The internal `EVALHUB_POST_PROCESSOR_DATA_PATHS` JSON environment variable maps
`results` and zero-based `calibration-N` names to their local paths. For example:

```json
{
  "results": "/mnt/evalhub/post-processing/staged/results",
  "calibration-0": "/mnt/evalhub/post-processing/pvc/calibration-0"
}
```

This is an internal runtime contract, not a new public API field. Existing
source references and data-format mappings remain unchanged. Git/HF revision
metadata and HF caches are kept outside the consumed dataset directories.
MLflow/OCI results continue to download on demand through the sidecar, including
artifacts resolved from `eval_job` references; they are not init-staged.

Deploy the corresponding contrib post-processor together with the enhanced
eval-hub service/init image. Local post-processor execution retains its existing
source download fallback. Normal evaluation jobs continue using the existing
single-source init interface.

Post-processing jobs use these APIs:

| Method | Path | Purpose |
| --- | --- | --- |
| POST | `/api/v1/evaluations/post-processing` | Create a standalone post-processing job. |
| GET | `/api/v1/evaluations/post-processing/{post_processing_job_id}` | Retrieve its status and results. |
| DELETE | `/api/v1/evaluations/post-processing/{post_processing_job_id}` | Delete the post-processing job. |

## Create a job

Edit [request.json](request.json) to reference a completed evaluation with
per-example results and your calibration data. The evaluation ID goes in
`operations.confidence_interval.results_data_ref.eval_job.id`. Set the calibration
S3 bucket, key, secret reference, and column names to match your data.

Run these commands from the repository root, using the same tenant and request
identity as for the source evaluation:

```sh
EVALHUB_URL=http://localhost:8080
EVALHUB_TENANT=my-namespace
EVALHUB_USER=my-user

curl --fail-with-body -sS -X POST \
  "$EVALHUB_URL/api/v1/evaluations/post-processing" \
  -H "X-Tenant: $EVALHUB_TENANT" \
  -H "X-User: $EVALHUB_USER" \
  -H 'Content-Type: application/json' \
  --data-binary @examples/post-processing/request.json
```

The response is HTTP 202 and includes `resource.id`, the ID of the new
post-processing job. Use that ID to retrieve or delete this resource. The source
evaluation remains unchanged.

## Retrieve status and results

Replace the placeholder with `resource.id` from the POST response:

```sh
POST_PROCESSING_JOB_ID='<resource.id from POST response>'

curl --fail-with-body -sS \
  "$EVALHUB_URL/api/v1/evaluations/post-processing/$POST_PROCESSING_JOB_ID" \
  -H "X-Tenant: $EVALHUB_TENANT" \
  -H "X-User: $EVALHUB_USER"
```

GET returns the post-processing resource with its current status. When
`status.state` is `completed`, results for the referenced evaluation are returned
under `results.benchmarks`.

## Delete the job

```sh
curl --fail-with-body -sS -X DELETE \
  "$EVALHUB_URL/api/v1/evaluations/post-processing/$POST_PROCESSING_JOB_ID" \
  -H "X-Tenant: $EVALHUB_TENANT" \
  -H "X-User: $EVALHUB_USER"
```

A successful deletion returns HTTP 204.
