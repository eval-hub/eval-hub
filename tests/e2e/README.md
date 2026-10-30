# EvalHub end-to-end tests

Run end-to-end tests against the Kubernetes cluster selected by the current
kubeconfig context:

    kubectl config current-context
    make test-e2e

The target takes no Make variables. It uses the normal Kubernetes client
configuration, including KUBECONFIG when set. Tests discover ready EvalHub API
instances and run only cases whose runtime configuration and supporting cluster
resources are present. A skipped case prints which prerequisite was not found;
an unrelated cluster with no EvalHub instance is reported as skipped.

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
