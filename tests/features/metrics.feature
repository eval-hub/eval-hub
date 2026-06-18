Feature: Metrics Endpoint
  As a monitoring system
  I want to scrape Prometheus metrics
  So that I can monitor the service

  @metrics
  Scenario: Get Prometheus metrics
    Given the service is running
    When I send a GET request to "/metrics"
    Then the response code should be 200
    And the response should contain Prometheus metrics
    And the metrics should include "http_requests_total"
    And the metrics should include "http_request_duration_seconds"
    And the metrics should include "http_requests_in_flight"

  @metrics
  Scenario: Metrics are recorded for requests
    Given the service is running
    When I send a GET request to "/api/v1/health"
    And I send a GET request to "/metrics"
    Then the metrics should show request count for "/api/v1/health"
