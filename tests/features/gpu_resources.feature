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
    And I wait for the evaluation job status to be "running|completed"
    When I send a GET request to "/api/v1/evaluations/jobs/{id}"
    Then the pod for evaluation job "{id}" should have a GPU attached
    When I send a DELETE request to "/api/v1/evaluations/jobs/{id}?hard_delete=true"
    Then the response code should be 204

  @scenario-1b
  Scenario: GPU request without queue with nodeSelector for available GPU type
    Given the service is running
    And GPU node with label "nvidia.com/gpu.product=A100-SXM4-40GB" exists
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/gpu_job_no_queue_with_selector_a100.json"
    Then the response code should be 202
    And the response should contain the value "pending" at path "$.status.state"
    And the response should contain the value "evaluation_job_created" at path "$.status.message.message_code"
    And I wait for the Kubernetes Job to be created for evaluation job "{id}"
    Then the Job spec should have GPU request set to "1"
    And the Job spec should have GPU limit set to "1"
    And the Job spec should have nodeSelector "nvidia.com/gpu.product=A100-SXM4-40GB"
    And I wait for the evaluation job status to be "running|completed"
    When I send a GET request to "/api/v1/evaluations/jobs/{id}"
    Then the pod for evaluation job "{id}" should have a GPU attached
    And the pod for evaluation job "{id}" should be on a node with label "nvidia.com/gpu.product=A100-SXM4-40GB"
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
    And the Job spec should have nodeSelector "nvidia.com/gpu.product=H100-SXM5-80GB"
    And I wait up to "5m" for the evaluation job to have scheduling error
    When I send a GET request to "/api/v1/evaluations/jobs/{id}"
    Then the response code should be 200
    And the response should contain an error message about GPU not being available
    And the pod for evaluation job "{id}" should not be scheduled
    When I send a DELETE request to "/api/v1/evaluations/jobs/{id}?hard_delete=true"
    Then the response code should be 204

  @scenario-2a
  @kueue
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
    And I wait for the evaluation job status to be "running|completed"
    When I send a GET request to "/api/v1/evaluations/jobs/{id}"
    Then the pod for evaluation job "{id}" should have a GPU attached
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
    And I wait for the evaluation job status to be "running|completed"
    When I send a GET request to "/api/v1/evaluations/jobs/{id}"
    Then the pod for evaluation job "{id}" should have a GPU attached
    And the pod for evaluation job "{id}" should be on a node with label "nvidia.com/gpu.product=A100-SXM4-40GB"
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
    And I wait up to "5m" for the evaluation job to have scheduling error
    When I send a GET request to "/api/v1/evaluations/jobs/{id}"
    Then the response code should be 200
    And the response should contain an error message about GPU not being available in queue
    And the pod for evaluation job "{id}" should not be scheduled
    When I send a DELETE request to "/api/v1/evaluations/jobs/{id}?hard_delete=true"
    Then the response code should be 204
