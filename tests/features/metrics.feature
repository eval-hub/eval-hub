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
    And the metrics should include "http_server_request_duration"
    And the metrics should include "http_server_request_count"
    And the metrics should include "http_server_active_requests"

  @metrics
  Scenario: Metrics are recorded for requests
    Given the service is running
    When I send a GET request to "/api/v1/health"
    And I send a GET request to "/metrics"
    Then the metrics should show request count for "/api/v1/health"

  @metrics
  Scenario: Evaluation domain metrics are registered
    Given the service is running
    When I send a GET request to "/api/v1/health"
    And I send a GET request to "/metrics"
    Then the response code should be 200
    And the metrics should include "evalhub_evaluation_jobs_active"
    And the metrics should include "evalhub_evaluation_queue_depth"
    And the metrics should include "evalhub_api_request_duration_seconds"

  @metrics
  Scenario: API request duration has enriched labels
    Given the service is running
    When I send a GET request to "/api/v1/health"
    And I send a GET request to "/metrics"
    Then the metrics should include "evalhub_api_request_duration_seconds"
    And the metrics should show request count for "/api/v1/health"
