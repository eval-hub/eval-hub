Feature: Providers Endpoint
  As a user
  I want to query the supported providers
  So that I discover the service capabilities

  Scenario: Get all providers
    Given the service is running
    When I send a GET request to "/api/v1/evaluations/providers"
    Then the response code should be 200

  Scenario: Get providers for non existent provider_id
    Given the service is running
    When I send a GET request to "/api/v1/evaluations/providers?provider_id=oops"
    Then the response should contain the value "0" at path "total_count"

  Scenario: Get provider for existent provider_id
    Given the service is running
    When I send a GET request to "/api/v1/evaluations/providers?provider_id=garak"
    Then the response should contain the value "1" at path "total_count"

  Scenario: Get provider without benchmarks
    Given the service is running
    When I send a GET request to "/api/v1/evaluations/providers?benchmarks=false"
    Then the response should contain the value "[]" at path "items[0].benchmarks"

