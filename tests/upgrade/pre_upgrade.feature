@pre-upgrade
Feature: Pre-Upgrade — Create Test Fixtures
  Create evaluation resources on the source RHOAI version that
  must survive the upgrade.

  Background:
    Given I set the header "Authorization" to "Bearer {{env:AUTH_TOKEN}}"
    And I set the header "X-Tenant" to "{{env:X_TENANT}}"
    And I set the header "X-User" to "{{env:X_USER|upgrade-test-user}}"
    And I set the wait deadline to "10m"
    And I set the wait interval to "10s"

  Scenario: Create evaluation job and wait for completion
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/pre_upgrade_job.jsonnet"
    Then the response code should be 202
    And the "resource.id" field in the response should be saved as "value:job_id"
    And I wait for the evaluation job status to be "completed"
    When I send a GET request to "/api/v1/evaluations/jobs/{id}"
    Then the response code should be 200
    And the response should contain the value "completed" at path "$.status.state"
    And the response should contain the value "arc_easy" at path "$.results.benchmarks[0].id"
    And the response should contain the value "{{value:job_id}}" at path "$.resource.id"
    And the response should contain the value "pre-upgrade-job1" at path "$.name"
    And I collect all jobs and save upgrade state to "{{env:UPGRADE_STATE_JSON}}" expecting current job "pre-upgrade-job1" in "completed" state
