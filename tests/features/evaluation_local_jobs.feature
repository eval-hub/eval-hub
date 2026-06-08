@local_runtime
@evaluations
Feature: Evaluation Jobs
  As a data scientist
  I want to create evaluation jobs against a running cluster
  So that I evaluate models

  Background:
    Given I set the header "X-Tenant" to "{{env:X_TENANT|test-tenant}}"
    And I set the header "X-User" to "{{env:X_USER|test-user}}"
    And I set the wait deadline to "{{env:WAIT_DEADLINE|30m}}"
    And the model endpoint is reachable

  @gha-wheel-sanity
  Scenario: Create an evaluation job and wait for completion
    Given the service is running
    And I set the wait interval to "5s"
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job.json"
    Then the response code should be 202
    And I wait for the evaluation job status to be "completed"
    When I send a DELETE request to "/api/v1/evaluations/jobs/{id}?hard_delete=true"
    Then the response code should be 204
    When I send a GET request to "/api/v1/evaluations/jobs/{id}"
    Then the response code should be 404
    And the response should contain the value "resource_not_found" at path "$.message_code"
    When I send a DELETE request to "/api/v1/evaluations/jobs/{id}?hard_delete=true"
    Then the response code should be 404
