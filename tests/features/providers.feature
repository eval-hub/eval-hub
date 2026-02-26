@providers
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
    When I send a GET request to "/api/v1/evaluations/providers/oops"
    Then the response code should be 404

  Scenario: Get provider for existent provider id
    Given the service is running
    When I send a GET request to "/api/v1/evaluations/providers/lm_evaluation_harness"
    Then the response code should be 200
    And the response should contain the value "lm_evaluation_harness" at path "resource.id"

  Scenario: Get provider without benchmarks
    Given the service is running
    When I send a GET request to "/api/v1/evaluations/providers?benchmarks=false"
    Then the response code should be 200
    Then the response should contain the value "[]" at path "items[0].benchmarks"

  Scenario: Create a user provider
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/providers" with body "file:/user_provider.json"
    Then the response code should be 201
    Then the response should contain the value "Test Provider" at path "name"
    Then the response should contain the value "A test provider" at path "description"
    When I send a GET request to "/api/v1/evaluations/providers/{id}"
    Then the response code should be 200
    Then the response should contain the value "Test Provider" at path "name"
    Then the response should contain the value "A test provider" at path "description"
    When I send a DELETE request to "/api/v1/evaluations/providers/{id}"
    Then the response code should be 204
