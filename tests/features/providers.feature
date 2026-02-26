@providers
Feature: Providers Endpoint
  As a user
  I want to query the supported providers
  So that I discover the service capabilities

  Scenario: Get all providers
    Given the service is running
    When I send a GET request to "/api/v1/evaluations/providers"
    Then the response code should be 200

  Scenario: List providers returns 200 with response structure
    Given the service is running
    When I send a GET request to "/api/v1/evaluations/providers"
    Then the response code should be 200
    And the response should contain "items"
    And the response should contain "limit"
    And the response should contain "total_count"

  Scenario: List providers with pagination params returns 200
    Given the service is running
    When I send a GET request to "/api/v1/evaluations/providers?limit=5&offset=0"
    Then the response code should be 200
    And the response should contain "items"
    And the response should contain "limit"
    And the response should contain "total_count"

  Scenario: List providers with invalid offset returns 400
    Given the service is running
    When I send a GET request to "/api/v1/evaluations/providers?offset=not-a-number"
    Then the response code should be 400
    And the response should contain the value "query_parameter_invalid" at path "message_code"

  Scenario: List providers with default params returns at least one provider
    Given the service is running
    When I send a GET request to "/api/v1/evaluations/providers"
    Then the response code should be 200
    And the array at path "items" in the response should have length at least 1

  Scenario: List providers includes system and user providers
    Given the service is running
    When I send a GET request to "/api/v1/evaluations/providers"
    Then the response code should be 200
    When I send a POST request to "/api/v1/evaluations/providers" with body "file:/user_provider.json"
    Then the response code should be 201
    When I send a GET request to "/api/v1/evaluations/providers"
    Then the response code should be 200
    And the array at path "items" in the response should have length at least 1
    When I send a DELETE request to "/api/v1/evaluations/providers/{id}"
    Then the response code should be 204

  Scenario: List providers with system_defined=false returns only user providers
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/providers" with body "file:/user_provider.json"
    Then the response code should be 201
    When I send a GET request to "/api/v1/evaluations/providers?system_defined=false"
    Then the response code should be 200
    And the array at path "items" in the response should have length 1
    And the response should contain the value "Test Provider" at path "items[0].name"
    When I send a DELETE request to "/api/v1/evaluations/providers/{id}"
    Then the response code should be 204

  Scenario: List providers with invalid limit returns 400
    Given the service is running
    When I send a GET request to "/api/v1/evaluations/providers?limit=-1"
    Then the response code should be 400
    And the response should contain the value "query_parameter_invalid" at path "message_code"

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

  Scenario: Update a user provider
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/providers" with body "file:/user_provider.json"
    Then the response code should be 201
    When I send a PUT request to "/api/v1/evaluations/providers/{id}" with body "file:/user_provider_update.json"
    Then the response code should be 200
    And the response should contain "name" with value "Updated Provider Name"
    And the response should contain "description" with value "Updated description for FVT"
    When I send a GET request to "/api/v1/evaluations/providers/{id}"
    Then the response code should be 200
    And the response should contain "name" with value "Updated Provider Name"
    And the response should contain "description" with value "Updated description for FVT"
    When I send a DELETE request to "/api/v1/evaluations/providers/{id}"
    Then the response code should be 204

  Scenario: Patch a user provider
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/providers" with body "file:/user_provider.json"
    Then the response code should be 201
    When I send a PATCH request to "/api/v1/evaluations/providers/{id}" with body "file:/user_provider_patch.json"
    Then the response code should be 200
    And the response should contain "name" with value "Patched Provider Name"
    And the response should contain "description" with value "Patched description for FVT"
    When I send a GET request to "/api/v1/evaluations/providers/{id}"
    Then the response code should be 200
    And the response should contain "name" with value "Patched Provider Name"
    And the response should contain "description" with value "Patched description for FVT"
    When I send a DELETE request to "/api/v1/evaluations/providers/{id}"
    Then the response code should be 204
