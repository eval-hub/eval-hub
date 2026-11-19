# EvalHub end-to-end tests

Run end-to-end tests against the Kubernetes cluster selected by the current
kubeconfig context:

    kubectl config current-context
    make test-e2e

The target automatically discovers the current Kubernetes context (or
`KUBECONFIG`) and deployed adapter selection. `E2E_TEST_TIMEOUT` can override
the overall Go test timeout; no adapter-selection argument is needed. Tests
discover ready EvalHub API instances and run only cases whose runtime
configuration and supporting cluster resources are present. A skipped case
prints which prerequisite was not found; an unrelated cluster with no EvalHub
instance is reported as skipped.

## Kind adapter image selection

`make dev-deploy` can build and deploy a subset of the curated-collection adapter
images. The default builds only FollowBench to keep the local deployment quick
and small. Select any supported subset explicitly, or use
`ADAPTERS=all` for all six:

    make dev-deploy AIRGAP=true ADAPTERS=inspect,lm-evaluation-harness
    make dev-deploy AIRGAP=true ADAPTERS=lighteval,ruler,ifbench,followbench
    make dev-deploy AIRGAP=true ADAPTERS=all

The supported names are `inspect`, `lm-evaluation-harness`, `lighteval`,
`ruler`, `ifbench`, and `followbench`. Only selected images are built and
published to the kind-local registry. The deployment records the selection,
image references, source revisions, and air-gap setting in
`system/evalhub-dev-deploy-manifest`. The test checks the resolved image ID from
each completed adapter Pod. The manifest contains no credentials.

A selected adapter can be supplied prebuilt instead of built from source, which
is useful when the adapter images already exist in a registry:

    make dev-deploy AIRGAP=true ADAPTERS=inspect \
      ADAPTER_IMAGES='inspect=quay.io/evalhub/community-inspect@sha256:...'

`ADAPTER_IMAGES` takes comma-separated `adapter=image` pairs, and each adapter
must also be selected by `ADAPTERS`. dev-deploy pulls the image into the
container runtime when it is not already present locally, re-tags it into the
kind-local registry, and records
`source.<adapter>.prebuilt=true` plus `source.<adapter>.image` in the manifest
instead of a build revision. The image checks above still apply, because the
registry reference, the provider ConfigMap, and the manifest entry are
identical to a built adapter. The pull runs on the host before the registry
push, so the adapter pull itself still works with `AIRGAP=true`, which restricts
egress from evaluation-job pods only. Add `SKIP_BUILD=true` to skip the pull and
require the adapter images to be present locally. That limits the offline
behaviour to adapter image resolution; it does not make the whole deployment
offline. A new `AIRGAP=true` cluster still downloads the pinned Calico manifest,
and dev-deploy still pulls the images it needs when they are absent: the kind
node image, `docker.io/library/registry:2`, `python:3.12-slim` for the mock
target, and `odh-kube-rbac-proxy`, which it clones and builds from source on
arm64.

`make adapter-images` prints that value for the selected adapters from the
references the provider configs declare, so a deploy can use the whole set
without typing each pair. Resolve the list first, so a failure to generate it
stops the deployment instead of continuing with an empty `ADAPTER_IMAGES`, and
pin `ADAPTERS=all` on the inner command so an exported narrower selection cannot
produce a subset of the pairs the deployment builds:

    adapter_images="$(make adapter-images ADAPTERS=all)" &&
      make dev-deploy AIRGAP=true ADAPTERS=all ADAPTER_IMAGES="$adapter_images"

Those images are published for `linux/amd64` only. On an arm64 machine, add
`ADAPTER_IMAGES_PLATFORM=linux/amd64` to pull them for emulation through the
podman machine's qemu-user-static; without it the pull fails with
`no image found in image index for architecture "arm64"`, and the alternative is
to leave the adapter out of `ADAPTER_IMAGES` so dev-deploy builds it for arm64.

For `ADAPTERS=all`, leave at least 4 GiB available for LightEval's pod memory
request in addition to the running service, operator, mocks, and Kubernetes
components. Increase the container VM's memory if the LightEval pod remains
Pending due to insufficient memory.

`make test-e2e` takes no adapter arguments. When this manifest is present, tests
run only for selected adapters. A deployment-level check verifies that every
selected adapter has an API provider configured with the selected image. The
BFCL and ToxiGen focused cases are retained for manually configured clusters;
for a `dev-deploy` cluster, the full curated matrix below covers those benchmarks
and reports runtime image provenance.

## Full curated-collection disconnected run

`TestCuratedCollectionsAirgap` defines 24 matrix cases for benchmark entries in
the nine currently curated collections. The number of cases run depends on the
deployed adapter selection, available catalog entries, and required credentials.
It checks the matrix against
this repository's collection YAMLs, discovers curated collections and providers
through the deployed API, verifies selected image wiring, and submits each
selected case serially. Duplicate BFCL entries in Tool Use v1 are separate cases
because their category lists differ.

| Adapter | Curated benchmark cases | Staged assets |
| --- | ---: | --- |
| Inspect | 5 | HLE (requires optional Hugging Face authentication), DocVQA, CyberSecEval 2, and the full BFCL cache |
| LM Evaluation Harness | 8 | The exact eight dataset bundles and a local tokenizer |
| LightEval | 3 | The three task datasets, `tinyBenchmarks.pkl`, staged NLTK `punkt` and `punkt_tab`, and a local tokenizer |
| RULER | 6 | The selected task inputs and a local tokenizer |
| IFBench | 1 | Packaged with the adapter image |
| FollowBench | 1 | Packaged with the adapter image; uses the mock judge endpoint |

The test creates one temporary PVC per data-backed adapter, stages assets with
that adapter's deployed image, runs its curated cases against the bundle, then
removes its staging pod and PVC before staging the next adapter. Inspect's BFCL
cache uses a separate PVC. This keeps bundles from accumulating on the cluster.
Staging Pods retain the mock Pod's non-root and read-only-root-filesystem
settings; all six supported adapter images declare a non-root final user. The
stagers write to `/test_data` and `/tmp`. LightEval also writes an installed
task-registry asset, so its staging Pod has a writable root filesystem.
The LightEval staging Pod downloads `punkt` and `punkt_tab` into
`/test_data/nltk_data` before importing the task registry. The evaluation Pod
uses that PVC through `test_data_ref` and does not download NLTK resources.
Every data-backed job request points to its staged PVC through benchmark-level
`test_data_ref`; the evaluation pod remains subject to the restrictive egress
policy. Jobs use the local mock target and judge services, assert successful
job and benchmark completion, and do not assert score quality. EvalHub job
records remain available for review; Kubernetes Jobs and Pods are removed after
their status and logs are read.

The HLE and LightEval GPQA datasets are gated on Hugging Face. To include these
rows, first accept each dataset's access conditions and provide an authorized
token in the local `HF_TOKEN` environment variable before running
`make test-e2e`. The test stores it in a temporary Kubernetes Secret and
exposes it only to the Inspect and LightEval asset-staging pods as needed; it
is not passed to evaluation jobs or printed. The Secret is deleted when the
test finishes. If `HF_TOKEN` is unset, the HLE and GPQA rows are skipped, while
the remaining selected matrix cases still run.

Automatic asset preparation requires staging pods to reach Hugging Face and
other upstream sources; only sources with a configured revision are pinned.
The policy installed by
`make dev-deploy AIRGAP=true` selects evaluation-job pods, not staging pods. This
verifies that benchmark jobs consume staged assets without external network
access; it cannot perform first-time staging in a physically isolated cluster.
The full matrix currently does not reuse pre-staged bundles, so a physically
disconnected cluster needs a separate asset pre-staging/reuse step before this
full run can be used there.

Use all six images for the complete matrix:

    make dev-deploy AIRGAP=true ADAPTERS=all
    make test-e2e

With a selected subset, the test automatically runs only matrix rows whose
adapter images were deployed. On a manually configured cluster without the
dev-deploy manifest, it discovers providers and runs matching cases; missing
curated rows are skipped, while unexpected catalog rows fail to prompt a matrix
update.

## Inspect BFCL air-gap case

The e2e case verifies that one BFCL example runs through the deployed Inspect
adapter while evaluation-job egress is restricted. It discovers the API,
Inspect provider, applicable egress policies, and local mock target. BFCL data
is handled automatically:

- If an Inspect BFCL collection already references a PVC, or a PVC is labeled
  for this test, the test reuses that asset source.
- Otherwise, the test fetches BFCL data from Gorilla commit
  dac44e7ac9db5ff26a01ab0c1ec5de5a1e703b7a on the machine running
  make test-e2e. It creates a temporary PVC and staging
  pod, copies the data into the claim, submits the evaluation using that PVC,
  and deletes the temporary staging pod and PVC afterward.

The cluster itself does not need GitHub access for this staging flow. The
machine running the test needs git, kubectl, Kubernetes permissions to create
and remove the temporary PVC and pod, and network access to GitHub when no
pre-staged asset is configured. The cluster needs a default StorageClass that
can provision a 1Gi PVC. The test stages data outside the evaluation pod's
restricted egress path.

Deploy the stack with Inspect selected and air-gap egress enabled:

    make dev-deploy AIRGAP=true ADAPTERS=inspect

Use the kubeconfig for that deployment as the active configuration. For the
standard dev-deploy path:

    export KUBECONFIG="$HOME/.config/evalhub/kubeconfigs/evalhub-inspect-airgap.yaml"
    kubectl config current-context

A manually staged BFCL claim remains supported as an offline override. Configure
it either in an Inspect BFCL collection's test_data_ref.pvc, or label the claim
as follows. Set the annotation only when the BFCL directory is below the PVC
root:

    kubectl -n system label pvc <claim-name> evalhub.io/e2e-inspect-bfcl-assets=true
    kubectl -n system annotate pvc <claim-name> evalhub.io/e2e-inspect-bfcl-sub-path=<sub-path>

If multiple distinct BFCL PVCs are discovered in one EvalHub namespace, the test
fails and lists them rather than choosing ambiguously.

The test creates a uniquely named short-lived mock-credential Secret, submits
one inspect/bfcl example using the simple_python category, and waits up to
20 minutes. It checks job and benchmark completion, not score quality. The
evaluation job remains in EvalHub for inspection; the temporary Secret,
staging pod, staging PVC, and port-forward process are removed by the test.

### Run and inspect

    make test-e2e

The test needs Kubernetes access to list pods, services, ConfigMaps,
NetworkPolicies, PVCs, and Endpoints; create a short-lived token for the
EvalHub API ServiceAccount; create/delete a temporary Secret, Pod, and PVC; and
use pods/exec to transfer BFCL files into the staging pod. The token is held in
process memory and is never printed. kubectl is used for the temporary API
port-forward and asset transfer.

A passing case prints the EvalHub namespace/service and resulting job ID.
Inspect the job in the UI or through the EvalHub jobs API. On failure, the test
includes the final job state and the last 40 adapter log lines.

The test skips when the cluster has no applicable EvalHub instance, Inspect
BFCL provider, mock target, or restrictive egress policy. If automatic data
download or PVC staging fails after those prerequisites are found, the test
fails with the fetch or Kubernetes error rather than silently skipping.


## LMES ToxiGen air-gap case

The e2e case verifies one ToxiGen example through the deployed LMES adapter while
evaluation-job egress is restricted. It creates a temporary PVC and a staging
pod using the configured LMES image. The stager downloads the public
`skg/toxigen-data` dataset (configuration `annotated`) and a small public
tokenizer, then saves the full DatasetDict at the dataset slug path expected by
LMES (`/test_data/skg--toxigen-data--annotated`). It verifies that bundle and the
tokenizer can be reopened locally before submitting a job. The PVC is mounted at
`/test_data`; the test job selects `/test_data/tokenizer` and references the
PVC through `test_data_ref`.

The staging pod is not selected by the evaluation-job egress policy and needs
access to Hugging Face to prepare assets. This simulates an administrator
staging assets before an evaluation in a disconnected environment. It requires
a default StorageClass able to provision a 3Gi PVC, and network access from the
staging pod to Hugging Face. The evaluation job itself remains subject to the
restrictive egress policy. A physically disconnected cluster cannot perform
automatic staging; it needs a pre-staged asset workflow before this case can
run.

The test also requires the local mock target deployed by `make dev-deploy` to
support OpenAI completions with log probabilities. The job and benchmark must
complete, and adapter logs must confirm offline mode and loading the staged
DatasetDict bundle. The adapter pod image and resolved image ID are reported;
the score is not asserted.
The EvalHub job record remains for inspection. After collecting the benchmark
status, adapter logs, and image ID, the test removes the Kubernetes Job and Pods
so the temporary credential Secret and PVC can be deleted without leaving the
large staged cache mounted. On failure, the test reports the logs before cleanup.

Deploy the LMES image with air-gap simulation enabled:

    make dev-deploy AIRGAP=true ADAPTERS=lm-evaluation-harness

Then run:

    make test-e2e
