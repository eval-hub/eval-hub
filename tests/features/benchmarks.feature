Feature: Evaluations Endpoint
  As a user
  I want to query the supported benchmarks
  So that I discover the service capabilities

  Scenario: Get all benchmarks
    Given the service is running
    When I send a GET request to "/api/v1/evaluations/benchmarks"
    Then the response code should be 200

  Scenario: Get benchmark for benchmark_id
    Given the service is running
    When I send a GET request to "/api/v1/evaluations/benchmarks?benchmark_id=oops"
    Then the response should contain the value "0" at path "total_count"

  Scenario: Get benchmark for benchmark_id and provider_id
    Given the service is running
    When I send a GET request to "/api/v1/evaluations/benchmarks?benchmark_id=toxicity&provider_id=garak"
    Then the response should contain the value "1" at path "total_count"


  Scenario: Get benchmarks for provider_id
    Given the service is running
    When I send a GET request to "/api/v1/evaluations/benchmarks?provider_id=garak"
    Then the response code should be 200
    And the response should contain the value "4" at path "total_count"

  Scenario: Get benchmarks for category
    Given the service is running
    When I send a GET request to "/api/v1/evaluations/benchmarks?category=code"
    Then the response code should be 200
    And the response should contain the value "8" at path "total_count"

  Scenario: Get benchmarks for tags
    Given the service is running
    When I send a GET request to "/api/v1/evaluations/benchmarks?tags=safety,toxicity"
    Then the response code should be 200
    And the response should contain the value "17" at path "total_count"