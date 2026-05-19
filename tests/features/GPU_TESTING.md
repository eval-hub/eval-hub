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

4. **GPU with queue, no nodeSelector**: Kueue assigns available GPU from ResourceFlavor
5. **GPU with queue, conflicting nodeSelector**: Kueue overrides nodeSelector with ResourceFlavor
6. **GPU with queue, no GPU quota**: Job cannot be scheduled due to missing GPU quota

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
cd /Users/nbs/work/ai-engineering/code/eval-hub
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

GPU test resources are automatically cleaned up after each scenario tagged with `@gpu`. Manual cleanup if needed:

```bash
oc delete localqueue test-local-queue cpu-local-queue -n ${X_TENANT}
oc delete clusterqueue gpu-cluster-queue cpu-only-cluster-queue
oc delete resourceflavor gpu-a100 gpu-v100 default-flavor
```

## Manual/Standalone Testing

The standalone test script deploys the feature images, sets up resources, runs all scenarios, and cleans up.

### Manual testing prerequisites

- All BDD test prerequisites above (see [Running BDD Tests](#running-bdd-tests))
- TrustyAI operator repository cloned at `../trustyai-service-operator`
- Feature images built and pushed:
  - eval-hub: `quay.io/rh-ee-nbs/nbs-dev:eval-hub_13may_1`
  - trustyai-operator: `quay.io/rh-ee-nbs/nbs-dev:sagar-trustyai-operator_13may_1`

### Run the Test Script

```bash
cd /Users/nbs/work/ai-engineering/code/eval-hub/tests/scripts

# Use default namespace (sagar)
./test_gpu_resources.sh

# Or specify a namespace
./test_gpu_resources.sh my-test-namespace
```

### What the Script Does

1. **Deploy feature images**: Uses `redeploy-evalhub.sh` to deploy eval-hub and operator
2. **Setup test resources**: Creates Kueue ClusterQueues, LocalQueues, ResourceFlavors, and node labels
3. **Deploy test provider**: Creates GPU test provider ConfigMap
4. **Run test scenarios**: Executes all 6 scenarios via API calls
5. **Validate results**: Checks Job specs, pod scheduling, GPU attachment
6. **Cleanup**: Removes all test resources
7. **Report results**: Summarizes pass/fail status

### Test Output

Results are saved to `tests/scripts/gpu_test_results_<timestamp>/`:
- `deploy.log` - Image deployment output
- `setup.log` - Resource setup output
- `scenario_*_create.json` - API responses for job creation
- `scenario_*_delete.json` - API responses for job deletion
- `cleanup.log` - Cleanup output

## Test Data Files

### Provider Configuration

`tests/features/test_data/provider_gpu_test.yaml` — Defines provider `gpu_test_provider`:

- Benchmark `arc_easy` (“Basic science Q&A (GPU test)”)
- Runtime GPU: `nvidia.com/gpu`, count `1` (no `node_selector`)

All `gpu_job_*.json` fixtures use benchmark `arc_easy`. Scenario-specific nodeSelectors (for example A100 or H100) come from other GPU test providers created in BDD setup (`gpu_test_provider_a100`, `gpu_test_provider_unavailable`), which also expose `arc_easy` only.

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

**Issue**: Provider not found
- **Solution**: Ensure `provider_gpu_test.yaml` is in the test_data directory and eval-hub is restarted

### Manual Script

**Issue**: "TrustyAI operator directory not found"
- **Solution**: Clone trustyai-service-operator to `../trustyai-service-operator`

**Issue**: Image pull errors
- **Solution**: Verify images are built and pushed, and cluster can access the registry

**Issue**: Evalhub pod not ready
- **Solution**: Check pod logs: `oc logs -n sagar -l app=eval-hub`

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
