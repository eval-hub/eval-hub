Feature: Benchmarks Endpoint
  As a user
  I want to query the supported benchmarks
  So that I discover the service capabilities

  Scenario: Get all benchmarks
    # Precondition: test environment has at least one benchmark.
    Given the service is running
    When I send a GET request to "/api/v1/evaluations/benchmarks"
    Then the response code should be 200
    And the response should have schema as:
    """
      {
        "properties": {
          "total_count": {
            "type": "integer",
            "minimum": 1
          },
          "items": {
            "type": "array",
            "minItems": 1
          }
        },
        "required": ["total_count", "items"]
      }
    """

  Scenario: Get benchmark for benchmark id
    Given the service is running
    When I send a GET request to "/api/v1/evaluations/benchmarks?id=oops"
    Then the response code should be 200
    Then the response should contain the value "0" at path "total_count"

  Scenario: Get benchmark for id and provider_id
    Given the service is running
    When I send a GET request to "/api/v1/evaluations/benchmarks?id=arc_easy&provider_id=lm_evaluation_harness"
    Then the response code should be 200
    Then the response should contain the value "1" at path "total_count"
    And the response should contain the value "arc_easy" at path "items[0].id"
    And the response should contain the value "lm_evaluation_harness" at path "items[0].provider_id"

  Scenario: Get benchmarks for provider_id
    Given the service is running
    When I send a GET request to "/api/v1/evaluations/benchmarks?provider_id=guidellm"
    Then the response code should be 200
    And the response should contain the value "7" at path "total_count"
    And the response should contain the value "guidellm" at path "items[0].provider_id"

  Scenario: Get benchmarks for category
    Given the service is running
    When I send a GET request to "/api/v1/evaluations/benchmarks?category=code"
    Then the response code should be 200
    And the response should contain the value "7" at path "total_count"

  Scenario: Get benchmarks for tags
    Given the service is running
    When I send a GET request to "/api/v1/evaluations/benchmarks?tags=safety,toxicity"
    Then the response code should be 200
    And the response should contain the value "18" at path "total_count"
