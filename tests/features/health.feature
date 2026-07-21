Feature: Health Check Endpoint
  As a service consumer
  I want to check the health of the service
  So that I can verify the service is running

  Scenario: Get healthz status for probes
    Given the service is running
    When I send a GET request to "/healthz"
    Then the response code should be 200

  Scenario: Get health status
    Given I set the header "X-Tenant" to "{{env:X_TENANT|test-tenant}}"
    And I set the header "X-User" to "{{env:X_USER|test-user}}"
    And the service is running
    When I send a GET request to "/api/v1/health"
    Then the response code should be 200
    And the response should be JSON
    And the response should contain "status" with value "healthy"
    And the response should contain "timestamp"

  @negative
  @cluster
  Scenario: Health endpoint rejects non-authenticated requests
    Given the service is running
    When I send a GET request to "/api/v1/health"
    Then the response code should be 400
    And the response should contain "message_code" with value "missing_"
    And the response should contain "message_code" with value "_header"

  @negative
  Scenario: Health endpoint rejects non-GET methods
    Given I set the header "X-Tenant" to "{{env:X_TENANT|test-tenant}}"
    And I set the header "X-User" to "{{env:X_USER|test-user}}"
    And the service is running
    When I send a POST request to "/api/v1/health"
    Then the response code should be 403
    When I send a PUT request to "/api/v1/health"
    Then the response code should be 403
    When I send a DELETE request to "/api/v1/health"
    Then the response code should be 403

  @negative
  Scenario: Healthz endpoint rejects non-GET methods
    Given the service is running
    When I send a POST request to "/healthz"
    Then the response code should be 405
