@info
Feature: Global service information
  As an EvalHub client
  I want to read service build information and tenant queues
  So that I can display the running service build and select a queue

  Background:
    Given I set the header "X-Tenant" to "{{env:X_TENANT|test-tenant}}"
    And I set the header "X-User" to "{{env:X_USER|test-user}}"

  Scenario: Get service build metadata and queue list
    Given the service is running
    When I send a GET request to "/api/v1/info"
    Then the response code should be 200
    And the response should be JSON
    And the response should contain "version"
    And the response should contain "build"
    And the response should contain "build_date"
    And the response should contain "git_hash"
    And the response should contain "queues"
    And the response at path "$.version" should not be empty
    And the response at path "$.build" should not be empty
    And the response at path "$.build_date" should not be empty
    And the array at path "$.queues" in the response should have length at least 0
    And service info metadata matches configured environment variables

  @cluster
  Scenario: Get the configured queue for the authenticated tenant
    Given the service is running
    When I send a GET request to "/api/v1/info"
    Then the response code should be 200
    And the response should be JSON
    And the configured tenant queue should be returned
