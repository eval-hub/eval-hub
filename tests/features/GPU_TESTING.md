# GPU Resource Management Testing

This document describes how to test the GPU resource management feature (RHAIRFE-2171).

## Feature Overview

The feature adds support for GPU resource allocation in eval-hub evaluation jobs, with integration for Kueue-based queue management. Key capabilities:

- Automatically sets GPU requests/limits based on provider benchmark configuration
- Supports nodeSelector for specific GPU types
- Integrates with Kueue for GPU quota management
- Handles conflicts between nodeSelector and Kueue ResourceFlavors

## Test Scenarios

### Without Queue (Direct Kubernetes Scheduling)

1. **GPU without nodeSelector**: Job requests GPU without specifying type
2. **GPU with nodeSelector (available)**: Job requests specific GPU type (A100) that exists
3. **GPU with nodeSelector (unavailable)**: Job requests GPU type (H100) that doesn't exist

### With Queue (Kueue-based Scheduling)

1. **GPU with queue, no nodeSelector**: Kueue assigns available GPU from ResourceFlavor
2. **GPU with queue, conflicting nodeSelector**: Kueue overrides nodeSelector with ResourceFlavor
3. **GPU with queue, no GPU quota**: Job cannot be scheduled due to missing GPU quota

## Running BDD Tests

The BDD tests assume the required versions of eval-hub and trustyai-operator are already deployed.

### Prerequisites

- OpenShift cluster with access
- Kueue installed (for queue-based scenarios)
- GPU nodes labeled (or test setup will add fake labels)
- `oc` CLI configured and logged in
- Environment variables:
  ```bash
  export X_TENANT=test-tenant  # or your test namespace
  export MODEL_URL=http://your-model-service
  export MODEL_NAME=test-model
  export MODEL_AUTH_SECRET_REF=test-secret
  ```

### Run All GPU Tests

```bash
cd "$(git rev-parse --show-toplevel)"   # or: cd <path-to-eval-hub-repo>
GODOG_TAGS="@gpu" go test -v ./tests/features/
```

### Run Specific Scenarios

```bash
# Scenario 1a: GPU without queue/nodeSelector
GODOG_TAGS="@scenario-1a" go test -v ./tests/features/

# Scenario 2a: GPU with queue
GODOG_TAGS="@scenario-2a" go test -v ./tests/features/

# All Kueue scenarios
GODOG_TAGS="@kueue" go test -v ./tests/features/
```

### Cleanup

GPU test resources are automatically cleaned up after each scenario tagged with `@gpu`. Optional cleanup if needed:

```bash
oc delete localqueue test-local-queue cpu-local-queue -n ${X_TENANT}
oc delete clusterqueue gpu-cluster-queue cpu-only-cluster-queue
oc delete resourceflavor gpu-a100 gpu-v100 default-flavor
```

## Test Data Files

### Provider Configuration

GPU test providers are created in suite `BeforeSuite` via `POST /api/v1/evaluations/providers` using:

- `tests/features/test_data/gpu_provider_test.json` — GPU without `node_selector`
- `tests/features/test_data/gpu_provider_a100.json` — GPU with A100 `node_selector`
- `tests/features/test_data/gpu_provider_unavailable.json` — GPU with H100 `node_selector` (negative tests)

Each provider is tagged with `gpu_fvt` for identification. The API returns tenant-scoped provider IDs, stored in suite-local state and referenced from job fixtures via:

- `{{env:GPU_TEST_PROVIDER_ID}}`
- `{{env:GPU_TEST_PROVIDER_A100_ID}}`
- `{{env:GPU_TEST_PROVIDER_UNAVAILABLE_ID}}`

These `{{env:...}}` placeholders resolve from suite state (not process environment), so parallel scenarios do not race on `os.Setenv`.

### Test Job Requests

- `gpu_job_no_queue_no_selector.json` - Scenario 1a
- `gpu_job_no_queue_with_selector_a100.json` - Scenario 1b
- `gpu_job_no_queue_with_selector_h100.json` - Scenario 1c
- `gpu_job_with_queue_no_selector.json` - Scenario 2a
- `gpu_job_with_queue_with_selector_v100.json` - Scenario 2b
- `gpu_job_with_queue_no_gpu_in_cq.json` - Scenario 2c

## Troubleshooting

### BDD Tests

**Issue**: Tests fail with "Kueue not installed"
- **Solution**: Install Kueue or skip `@kueue` tagged scenarios

**Issue**: Tests fail with "GPU not available"
- **Solution**: The test setup labels nodes with fake GPU types for testing. Real GPU hardware is not required for testing the job spec creation.

**Issue**: Provider not found / GPU test provider ID is not set
- **Solution**: Ensure suite setup completed (`BeforeSuite` creates providers via API). For cluster runs, set `SERVER_URL` and `AUTH_TOKEN` (or ensure `oc create token` works for `evalhub-service` in `X_TENANT`).

## Expected Results

### Successful Test Indicators

1. **Job specs**: GPU requests/limits set to `1` where expected
2. **NodeSelectors**: Set correctly for direct scheduling, omitted for Kueue
3. **Queue labels**: `kueue.x-k8s.io/queue-name` set for queued jobs
4. **Pod scheduling**: Pods scheduled when GPU available, pending when unavailable
5. **Error messages**: Appropriate error messages when GPU unavailable

### Known Limitations

- Tests use fake GPU labels on CPU nodes for validation
- Actual GPU hardware is not required for testing job spec correctness
- Some pods may remain in Pending state for unavailable GPU scenarios (expected behavior)

## Implementation Details

See the feature implementation:
- eval-hub image: `quay.io/rh-ee-nbs/nbs-dev:eval-hub_13may_1`
- trustyai-operator image: `quay.io/rh-ee-nbs/nbs-dev:sagar-trustyai-operator_13may_1`
- Jira ticket: https://redhat.atlassian.net/browse/RHAIRFE-2171
