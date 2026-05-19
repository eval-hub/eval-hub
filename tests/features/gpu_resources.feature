@cluster
@gpu
Feature: GPU Resource Management
  As a data scientist
  I want to run evaluation jobs with GPU requirements
  So that I can evaluate models that require GPU acceleration

  Background:
    Given I set the header "X-Tenant" to "{{env:X_TENANT|test-tenant}}"
    And I set the wait deadline to "{{env:WAIT_DEADLINE|30m}}"
    And the model endpoint is reachable
    And the value "{{env:MODEL_AUTH_SECRET_REF}}" is not empty
    And the GPU test provider is loaded

  @scenario-1a
  Scenario: GPU request without queue and without nodeSelector
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/gpu_job_no_queue_no_selector.json"
    Then the response code should be 202
    And the response should contain the value "pending" at path "$.status.state"
    And the response should contain the value "evaluation_job_created" at path "$.status.message.message_code"
    And I wait for the Kubernetes Job to be created for evaluation job "{id}"
    Then the Job spec should have GPU request set to "1"
    And the Job spec should have GPU limit set to "1"
    And the Job spec should not have nodeSelector
    When I send a DELETE request to "/api/v1/evaluations/jobs/{id}?hard_delete=true"
    Then the response code should be 204

  @scenario-1b
  # Validates Job spec for GPU with nodeSelector (no queue)
  # Note: Execution validation would require actual GPU nodes and is not included
  # to keep tests fast and avoid dependency on physical GPU hardware
  Scenario: GPU request without queue with nodeSelector for available GPU type
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/gpu_job_no_queue_with_selector_a100.json"
    Then the response code should be 202
    And the response should contain the value "pending" at path "$.status.state"
    And the response should contain the value "evaluation_job_created" at path "$.status.message.message_code"
    And I wait for the Kubernetes Job to be created for evaluation job "{id}"
    Then the Job spec should have GPU request set to "1"
    And the Job spec should have GPU limit set to "1"
    And the Job spec should have nodeSelector "nvidia.com/gpu.product=A100-SXM4-40GB"
    When I send a DELETE request to "/api/v1/evaluations/jobs/{id}?hard_delete=true"
    Then the response code should be 204

  @scenario-1c
  Scenario: GPU request without queue with nodeSelector for unavailable GPU type
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/gpu_job_no_queue_with_selector_h100.json"
    Then the response code should be 202
    And the response should contain the value "pending" at path "$.status.state"
    And the response should contain the value "evaluation_job_created" at path "$.status.message.message_code"
    And I wait for the Kubernetes Job to be created for evaluation job "{id}"
    Then the Job spec should have GPU request set to "1"
    And the Job spec should have GPU limit set to "1"
    And the Job spec should have nodeSelector "nvidia.com/gpu.product=NVIDIA-H100-80GB-HBM3"
    When I send a DELETE request to "/api/v1/evaluations/jobs/{id}?hard_delete=true"
    Then the response code should be 204

  @scenario-2a
  @kueue
  # Validates Job spec for GPU with Kueue queue
  # Note: Execution validation would require GPU quota and admission, not included
  # to keep tests fast and avoid dependency on Kueue scheduling completion
  Scenario: GPU request with queue without nodeSelector
    Given the service is running
    And Kueue is installed on the cluster
    And ClusterQueue "gpu-cluster-queue" with GPU ResourceFlavor exists
    And LocalQueue "test-local-queue" in namespace "{{env:X_TENANT|test-tenant}}" exists
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/gpu_job_with_queue_no_selector.json"
    Then the response code should be 202
    And the response should contain the value "pending" at path "$.status.state"
    And the response should contain the value "evaluation_job_created" at path "$.status.message.message_code"
    And I wait for the Kubernetes Job to be created for evaluation job "{id}"
    Then the Job spec should have GPU request set to "1"
    And the Job spec should have GPU limit set to "1"
    And the Job spec should have label "kueue.x-k8s.io/queue-name=test-local-queue"
    When I send a DELETE request to "/api/v1/evaluations/jobs/{id}?hard_delete=true"
    Then the response code should be 204

  @scenario-2b
  @kueue
  Scenario: GPU request with queue with nodeSelector that conflicts with ResourceFlavor
    Given the service is running
    And Kueue is installed on the cluster
    And ClusterQueue "gpu-cluster-queue" with GPU ResourceFlavor "gpu-a100" exists
    And ResourceFlavor "gpu-a100" has nodeSelector "nvidia.com/gpu.product=A100-SXM4-40GB"
    And LocalQueue "test-local-queue" in namespace "{{env:X_TENANT|test-tenant}}" exists
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/gpu_job_with_queue_with_selector_v100.json"
    Then the response code should be 202
    And the response should contain the value "pending" at path "$.status.state"
    And the response should contain the value "evaluation_job_created" at path "$.status.message.message_code"
    And I wait for the Kubernetes Job to be created for evaluation job "{id}"
    Then the Job spec should have GPU request set to "1"
    And the Job spec should have GPU limit set to "1"
    And the Job spec should not have nodeSelector
    And the Job spec should have label "kueue.x-k8s.io/queue-name=test-local-queue"
    When I send a DELETE request to "/api/v1/evaluations/jobs/{id}?hard_delete=true"
    Then the response code should be 204

  @scenario-2c
  @kueue
  Scenario: GPU request with queue but no GPU in ClusterQueue
    Given the service is running
    And Kueue is installed on the cluster
    And ClusterQueue "cpu-only-cluster-queue" without GPU ResourceFlavor exists
    And LocalQueue "cpu-local-queue" in namespace "{{env:X_TENANT|test-tenant}}" exists
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/gpu_job_with_queue_no_gpu_in_cq.json"
    Then the response code should be 202
    And the response should contain the value "pending" at path "$.status.state"
    And the response should contain the value "evaluation_job_created" at path "$.status.message.message_code"
    And I wait for the Kubernetes Job to be created for evaluation job "{id}"
    Then the Job spec should have GPU request set to "1"
    And the Job spec should have GPU limit set to "1"
    And the Job spec should have label "kueue.x-k8s.io/queue-name=cpu-local-queue"
    When I send a DELETE request to "/api/v1/evaluations/jobs/{id}?hard_delete=true"
    Then the response code should be 204
