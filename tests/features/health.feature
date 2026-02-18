Feature: Health Check Endpoint
  As a service consumer
  I want to check the health of the service
  So that I can verify the service is running

  Scenario: Get health status
    Given the service is running
    When I send a GET request to "/api/v1/health"
    Then the response code should be 200
    And the response should be JSON
    And the response should contain "status" with value "healthy"
    And the response should contain "timestamp"

  Scenario: Health endpoint rejects non-GET methods
    Given the service is running
    When I send a POST request to "/api/v1/health"
    Then the response code should be 405
    When I send a PUT request to "/api/v1/health"
    Then the response code should be 405
    When I send a DELETE request to "/api/v1/health"
    Then the response code should be 405
