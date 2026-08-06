@cluster
@k8s_lifecycle
Feature: Kubernetes Lifecycle Signals
  As a platform operator
  I want evaluation Jobs to emit Kubernetes Events and carry an evaluation-phase label
  So that I can observe evaluation lifecycle transitions without direct EvalHub API access

  Background:
    Given I set the header "X-Tenant" to "{{env:X_TENANT|test-tenant}}"
    And I set the header "X-User" to "{{env:X_USER|test-user}}"
    And I set the wait deadline to "{{env:WAIT_DEADLINE|30m}}"
    And the model endpoint is reachable
    And the value "{{env:MODEL_AUTH_SECRET_REF}}" is not empty

  Scenario: Kubernetes Events are emitted for a completed evaluation job
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job.json"
    Then the response code should be 202
    And I wait for the evaluation job status to be "completed"
    Then I observe a Kubernetes Event with reason "EvaluationRunning" for the evaluation job within "60s"
    And I observe a Kubernetes Event with reason "EvaluationCompleted" for the evaluation job within "60s"
    When I send a DELETE request to "/api/v1/evaluations/jobs/{id}?hard_delete=true"
    Then the response code should be 204

  Scenario: Evaluation Job carries the evaluation-phase label at each lifecycle state
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job.json"
    Then the response code should be 202
    And I wait for the Kubernetes evaluation Job to be created
    And I wait for the evaluation Job to have label "trustyai.opendatahub.io/evaluation-phase" equal to "Pending" within "30s"
    And I wait for the evaluation Job to have label "trustyai.opendatahub.io/evaluation-phase" equal to "Running" within "5m"
    And I wait for the evaluation job status to be "completed"
    And the evaluation Job should have label "trustyai.opendatahub.io/evaluation-phase" equal to "Completed"
    When I send a DELETE request to "/api/v1/evaluations/jobs/{id}?hard_delete=true"
    Then the response code should be 204

  @negative
  Scenario: EvaluationFailed event is emitted when an evaluation job fails
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evalcard_invalid_model.json"
    Then the response code should be 202
    And I wait for the evaluation job status to match "completed|failed"
    Then I observe a Kubernetes Event with reason "EvaluationFailed" for the evaluation job within "60s"
    And the evaluation Job should have label "trustyai.opendatahub.io/evaluation-phase" equal to "Failed"
    When I send a DELETE request to "/api/v1/evaluations/jobs/{id}?hard_delete=true"
    Then the response code should be 204

