@providers
Feature: Providers Endpoint
  As a user
  I want to query the supported providers
  So that I discover the service capabilities

  Background:
    Given I set the header "X-Tenant" to "{{env:X_TENANT|test-tenant}}"
    And I set the header "X-User" to "{{env:X_USER|test-user}}"

  Scenario: List providers returns 200 with response structure and pagination
    Given the service is running
    When I send a GET request to "/api/v1/evaluations/providers?limit=5&offset=0"
    Then the response code should be 200
    And the response should contain "items"
    And the response should contain "limit"
    And the response should contain "total_count"

  @negative
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

  Scenario: List providers with scope=tenant returns only user providers
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/providers" with body "file:/user_provider.json"
    Then the response code should be 201
    When I send a GET request to "/api/v1/evaluations/providers?scope=tenant"
    Then the response code should be 200
    And the array at path "items" in the response should have length at least 1
    # TODO And the response should contain the value "Test Provider" at path "items[0].name"
    When I send a DELETE request to "/api/v1/evaluations/providers/{id}"
    Then the response code should be 204

  Scenario: List providers with scope=system returns only system providers
    Given the service is running
    And there are system providers
    When I send a GET request to "/api/v1/evaluations/providers?scope=system"
    Then the response code should be 200
    And the array at path "items" in the response should have length at least 1
    And the response should contain the value "system" at path "items[0].resource.owner"

  Scenario: List providers with no returns all providers
    Given the service is running
    And there are system providers
    When I send a POST request to "/api/v1/evaluations/providers" with body "file:/user_provider.json"
    Then the response code should be 201
    When I send a POST request to "/api/v1/evaluations/providers" with body "file:/user_provider.json"
    Then the response code should be 201
    When I send a GET request to "/api/v1/evaluations/providers"
    Then the response code should be 200
    And the response should contain at least the value "3" at path "total_count"
    And the array at path "items" in the response should have length at least 3

  @negative
  Scenario: List providers with invalid limit returns 400
    Given the service is running
    When I send a GET request to "/api/v1/evaluations/providers?limit=-1"
    Then the response code should be 400
    And the response should contain the value "query_parameter_invalid" at path "message_code"

  @negative
  Scenario: List providers with invalid scope returns 400
    Given the service is running
    When I send a GET request to "/api/v1/evaluations/providers?scope=invalid"
    Then the response code should be 400
    And the response should contain the value "query_parameter_value_invalid" at path "$.message_code"

  Scenario: List system providers with pagination
    Given the service is running
    And there are system providers
    # This will skip the scenario if there are no system providers
    When I send a GET request to "/api/v1/evaluations/providers?limit=50&offset=0&scope=system"
    Then the response code should be 200
    And the response should contain the value "50" at path "$.limit"
    And the "total_count" field in the response should be saved as "value:num_providers"
    When I send a GET request to "/api/v1/evaluations/providers?limit=50&offset=0&scope=system"
    Then the response code should be 200
    And the response should contain the value "50" at path "$.limit"
    And the array at path "items" in the response should have length "{{value:num_providers}}"
    And the response should contain the value "{{value:num_providers}}" at path "$.total_count"
    When I send a GET request to "/api/v1/evaluations/providers?limit=50&offset=1&scope=system"
    Then the response code should be 200
    And the response should contain the value "50" at path "$.limit"
    And the array at path "items" in the response should have length at least "{{value:num_providers}}-1"
    And the response should contain the value "{{value:num_providers}}" at path "$.total_count"
    When I send a GET request to "/api/v1/evaluations/providers?limit=50&offset=2&scope=system"
    Then the response code should be 200
    And the response should contain the value "50" at path "$.limit"
    And the array at path "items" in the response should have length at least "{{value:num_providers}}-2"
    And the response should contain the value "{{value:num_providers}}" at path "$.total_count"
    When I send a GET request to "/api/v1/evaluations/providers?limit=50&offset={{value:num_providers}}&scope=system"
    Then the response code should be 200
    And the response should contain the value "50" at path "$.limit"
    And the array at path "items" in the response should have length 0
    And the response should contain the value "{{value:num_providers}}" at path "$.total_count"

  Scenario: List user providers with pagination
    Given the service is running
    When I send a GET request to "/api/v1/evaluations/providers?limit=50&offset=0&scope=system"
    Then the response code should be 200
    And the "total_count" field in the response should be saved as "value:num_providers"
    When I send a POST request to "/api/v1/evaluations/providers" with body "file:/user_provider.json"
    Then the response code should be 201
    And the "resource.id" field in the response should be saved as "value:provider1_id"
    When I send a GET request to "/api/v1/evaluations/providers?limit=50&offset=0&scope=system"
    Then the response code should be 200
    And the response should contain at least the value "{{value:num_providers}}" at path "$.total_count"
    When I send a GET request to "/api/v1/evaluations/providers?limit=50&offset={{value:num_providers}}"
    Then the response code should be 200
    And the response should contain the value "50" at path "$.limit"
    And the array at path "items" in the response should have length at least 1
    When I send a POST request to "/api/v1/evaluations/providers" with body "file:/user_provider.json"
    Then the response code should be 201
    And the "resource.id" field in the response should be saved as "value:provider2_id"
    When I send a GET request to "/api/v1/evaluations/providers?limit=50&offset=3"
    Then the response code should be 200
    And the response should contain the value "50" at path "$.limit"
    And the array at path "items" in the response should have length at least "{{value:num_providers}} - 1"
    When I send a GET request to "/api/v1/evaluations/providers?limit=1&scope=tenant"
    Then the response code should be 200
    And the response should contain at least the value "1" at path "$.limit"
    And the array at path "items" in the response should have length at least 1
    # TODO does not work with existing database or when run in parallel
    # And the response should contain the value "{{value:provider1_id}}|{{value:provider2_id}}" at path "$.items[0].resource.id"

  Scenario: List providers with all search parameters and pagination
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/providers" with body "file:/user_provider.json"
    Then the response code should be 201
    And the "resource.id" field in the response should be saved as "value:provider1_id"
    When I send a POST request to "/api/v1/evaluations/providers" with body "file:/user_provider_with_tags.json"
    Then the response code should be 201
    And the "resource.id" field in the response should be saved as "value:provider2_id"
    When I send a GET request to "/api/v1/evaluations/providers?limit=2&offset=0&scope=tenant"
    Then the response code should be 200
    And the response should contain the value "2" at path "$.limit"
    And the array at path "items" in the response should have length at least 2
    And the response should contain at least the value "2" at path "$.total_count"
    And the response should not contain the value "next" at path "$.next"
    When I send a GET request to "/api/v1/evaluations/providers?limit=5&offset=0"
    Then the response code should be 200
    And the response should contain at least the value "5" at path "$.limit"
    And the array at path "items" in the response should have length at least 4
    When I send a GET request to "/api/v1/evaluations/providers?limit=2&offset=1&scope=tenant"
    Then the response code should be 200
    And the response should contain at least the value "2" at path "$.limit"
    And the array at path "items" in the response should have length at least 1
    When I send a GET request to "/api/v1/evaluations/providers?limit=100&offset=0"
    Then the response code should be 200
    And the response should contain the value "100" at path "$.limit"
    When I send a GET request to "/api/v1/evaluations/providers?benchmarks=false&limit=5"
    Then the response code should be 200
    And the response should not equal the value "0" at path "$.total_count"
    And the response should contain the value "[]" at path "items[0].benchmarks"
    When I send a GET request to "/api/v1/evaluations/providers?tags=nonexistent-tag&limit=10"
    Then the response code should be 200
    And the response should contain the value "0" at path "$.total_count"
    When I send a GET request to "/api/v1/evaluations/providers?scope=tenant&limit=10"
    Then the response code should be 200
    And the array at path "items" in the response should have length at least 2
    And the response should contain at least the value "2" at path "$.total_count"
    When I send a DELETE request to "/api/v1/evaluations/providers/{{value:provider1_id}}"
    Then the response code should be 204
    When I send a DELETE request to "/api/v1/evaluations/providers/{{value:provider2_id}}"
    Then the response code should be 204
    When I send a GET request to "/api/v1/evaluations/providers/lighteval"
    Then the response code should be 200
    And the response should contain the value "Lighteval" at path "$.name"
    When I send a GET request to "/api/v1/evaluations/providers?name=Lighteval&limit=10"
    Then the response code should be 200
    And the response should contain the value "1" at path "$.total_count"
    And the response should contain the value "Lighteval" at path "$.items[0].name"

  Scenario: List providers with comprehensive search parameters and pagination
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/providers" with body "file:/user_provider.json"
    Then the response code should be 201
    And the "resource.id" field in the response should be saved as "value:list_prov1_id"
    When I send a POST request to "/api/v1/evaluations/providers" with body "file:/user_provider_with_tags.json"
    Then the response code should be 201
    And the "resource.id" field in the response should be saved as "value:list_prov2_id"
    When I send a POST request to "/api/v1/evaluations/providers" with body "file:/user_provider.json"
    Then the response code should be 201
    And the "resource.id" field in the response should be saved as "value:list_prov3_id"
    When I send a GET request to "/api/v1/evaluations/providers?name=Lighteval&limit=10"
    Then the response code should be 200
    And the response should contain the value "Lighteval" at path "$.items[0].name"
    And the response should contain the value "1" at path "$.total_count"
    When I send a GET request to "/api/v1/evaluations/providers?name=LM%20Evaluation%20Harness&limit=10"
    Then the response code should be 200
    And the response should contain the value "LM Evaluation Harness" at path "$.items[0].name"
    And the response should contain the value "1" at path "$.total_count"
    When I send a GET request to "/api/v1/evaluations/providers?scope=tenant&limit=10"
    Then the response code should be 200
    And the response should contain at least the value "3" at path "$.total_count"
    When I send a GET request to "/api/v1/evaluations/providers?tags=nonexistent-tag&limit=10"
    Then the response code should be 200
    And the response should contain the value "0" at path "$.total_count"
    When I send a GET request to "/api/v1/evaluations/providers?limit=1&offset=0"
    Then the response code should be 200
    And the response should contain the value "1" at path "$.limit"
    And the array at path "items" in the response should have length 1
    And the response should contain "next"
    When I send a GET request to "/api/v1/evaluations/providers?limit=1&offset=1"
    Then the response code should be 200
    And the response should contain the value "1" at path "$.limit"
    And the array at path "items" in the response should have length 1
    When I send a GET request to "/api/v1/evaluations/providers?limit=2&offset=0"
    Then the response code should be 200
    And the response should contain at least the value "2" at path "$.limit"
    And the array at path "items" in the response should have length 2
    When I send a GET request to "/api/v1/evaluations/providers?limit=2&offset=1"
    Then the response code should be 200
    And the array at path "items" in the response should have length 2
    When I send a GET request to "/api/v1/evaluations/providers?limit=2&offset=2"
    Then the response code should be 200
    And the array at path "items" in the response should have length at least 1
    When I send a GET request to "/api/v1/evaluations/providers?limit=5&offset=0"
    Then the response code should be 200
    And the response should contain the value "5" at path "$.limit"
    When I send a GET request to "/api/v1/evaluations/providers?limit=50&offset=0"
    Then the response code should be 200
    And the response should contain the value "50" at path "$.limit"
    When I send a GET request to "/api/v1/evaluations/providers?limit=100&offset=0"
    Then the response code should be 200
    And the response should contain the value "100" at path "$.limit"
    When I send a GET request to "/api/v1/evaluations/providers?benchmarks=false&limit=5"
    Then the response code should be 200
    And the array at path "items" in the response should have length at least 3
    And the response should not equal the value "0" at path "$.total_count"
    And the response should contain the value "[]" at path "items[0].benchmarks"
    When I send a GET request to "/api/v1/evaluations/providers?benchmarks=true&limit=5"
    Then the response code should be 200
    And the array at path "items[0].benchmarks" in the response should have length at least 1
    When I send a GET request to "/api/v1/evaluations/providers?limit=2&name=Test%20Provider&benchmarks=false"
    Then the response code should be 200
    And the response should contain the value "2" at path "$.limit"
    And the array at path "items" in the response should have length at least 1
    And the response should not equal the value "0" at path "$.total_count"
    And the response should contain the value "[]" at path "items[0].benchmarks"
    And the response should contain the value "Test Provider" at path "$.items[0].name"
    When I send a DELETE request to "/api/v1/evaluations/providers/{{value:list_prov1_id}}"
    Then the response code should be 204
    When I send a DELETE request to "/api/v1/evaluations/providers/{{value:list_prov2_id}}"
    Then the response code should be 204
    When I send a DELETE request to "/api/v1/evaluations/providers/{{value:list_prov3_id}}"
    Then the response code should be 204

  @negative
  Scenario: Get providers for non existent provider_id
    Given the service is running
    When I send a GET request to "/api/v1/evaluations/providers/oops"
    Then the response code should be 404

  Scenario: Get provider for existent provider id
    Given the service is running
    When I send a GET request to "/api/v1/evaluations/providers/lm_evaluation_harness"
    Then the response code should be 200
    And the response should contain the value "lm_evaluation_harness" at path "resource.id"

  Scenario: List providers without benchmarks excludes benchmark data
    Given the service is running
    When I send a GET request to "/api/v1/evaluations/providers?benchmarks=false"
    Then the response code should be 200
    And the response should not equal the value "0" at path "$.total_count"
    And the response should contain the value "[]" at path "items[0].benchmarks"

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
    When I send a PATCH request to "/api/v1/evaluations/providers/{id}" with body:
    """
    [{"op":"replace","path":"/runtime","value":{"local": {"command": "echo 'hello'"}}}]
    """
    Then the response code should be 200
    And the response should contain the value "echo 'hello'" at path "$.runtime.local.command"
    When I send a PATCH request to "/api/v1/evaluations/providers/{id}" with body:
    """
    [{"op":"replace","path":"/runtime/local/command","value":"echo 'goodbye'"}]
    """
    Then the response code should be 200
    And the response should contain the value "echo 'goodbye'" at path "$.runtime.local.command"
    When I send a PATCH request to "/api/v1/evaluations/providers/{id}" with body:
    """
    [{"op":"add","path":"/tags","value":["foo", "bar"]}]
    """
    Then the response code should be 200
    And the response should contain the value "foo" at path "tags"
    And the response should contain the value "bar" at path "tags"
    When I send a PATCH request to "/api/v1/evaluations/providers/{id}" with body:
    """
    [{"op":"replace","path":"/tags","value":["foo", "tree"]}]
    """
    Then the response code should be 200
    And the response should contain the value "foo" at path "tags"
    And the response should contain the value "tree" at path "tags"
    And the response should not contain the value "bar" at path "tags"
    When I send a DELETE request to "/api/v1/evaluations/providers/{id}"
    Then the response code should be 204

  @negative
  Scenario: Update system provider returns 400
    Given the service is running
    When I send a PUT request to "/api/v1/evaluations/providers/lm_evaluation_harness" with body "file:/user_provider_update.json"
    Then the response code should be 400
    And the response should contain the value "read_only_provider" at path "message_code"

  @negative
  Scenario: Patch provider with invalid operation returns 400
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/providers" with body "file:/user_provider.json"
    Then the response code should be 201
    When I send a PATCH request to "/api/v1/evaluations/providers/{id}" with body:
    """
    [{"op":"invalid_op","path":"/name","value":"x"}]
    """
    Then the response code should be 400
    And the response should contain the value "invalid_patch_operation" at path "message_code"
    And the response should contain the value "Allowed operations are" at path "message"
    And the response should not contain the value "Allowed operations areJ" at path "message"
    And the response should contain the value "replace" at path "message"
    And the response should contain the value "add" at path "message"
    And the response should contain the value "remove" at path "message"

  @negative
  Scenario: Patch system provider returns 400
    Given the service is running
    When I send a PATCH request to "/api/v1/evaluations/providers/lm_evaluation_harness" with body "file:/user_provider_patch.json"
    Then the response code should be 400
    And the response should contain the value "read_only_provider" at path "message_code"

  @negative
  Scenario: Update non-existent provider returns 404
    Given the service is running
    When I send a PUT request to "/api/v1/evaluations/providers/00000000-0000-0000-0000-000000000000" with body "file:/user_provider_update.json"
    Then the response code should be 404

  @negative
  Scenario: Patch non-existent provider returns 404
    Given the service is running
    When I send a PATCH request to "/api/v1/evaluations/providers/00000000-0000-0000-0000-000000000000" with body "file:/user_provider_patch.json"
    Then the response code should be 404

  @negative
  Scenario: Update provider with empty path returns 404
    Given the service is running
    When I send a PUT request to "/api/v1/evaluations/providers/" with body "file:/user_provider_update.json"
    # 403 is returned by the kube-rbac-proxy - 404 is standalone
    Then the response code should be 404 or 403

  @negative
  Scenario: Get provider with empty path returns 404
    Given the service is running
    When I send a GET request to "/api/v1/evaluations/providers/"
    Then the response code should be 404

  @negative
  Scenario: Patch provider with invalid patch returns 400
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/providers" with body "file:/user_provider.json"
    Then the response code should be 201
    When I send a PATCH request to "/api/v1/evaluations/providers/{id}" with body:
    """
    [{"op":"replace","path":"/resource/id","value":"hacked-id"}]
    """
    Then the response code should be 400
    And the response should contain the value "unallowed_patch" at path "message_code"
    And the response should contain the value "is not allowed" at path "message"
    When I send a PATCH request to "/api/v1/evaluations/providers/{id}" with body:
    """
    [{"op":"remove","path":"/name"}]
    """
    Then the response code should be 400
    And the response should contain the value "unallowed_patch" at path "message_code"
    And the response should contain the value "The operation 'remove' is not allowed for the path '/name'" at path "message"

  Scenario: List providers by tags and name
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/providers" with body:
    """
    {
      "name": "test-provider-1",
      "description": "Test provider 1 for FVT",
      "tags": ["test-tag-1", "test-tag-2"],
      "benchmarks": [
        {
            "id": "arc_easy",
            "name": "lm_evaluation_harness"
        }
      ]
    }
    """
    Then the response code should be 201
    When I send a POST request to "/api/v1/evaluations/providers" with body:
    """
    {
      "name": "test-provider-2",
      "description": "Test provider 2 for FVT",
      "tags": ["test-tag-1"],
      "benchmarks": [
        {
            "id": "arc_easy",
            "name": "lm_evaluation_harness"
        }
      ]
    }
    """
    Then the response code should be 201
    When I send a POST request to "/api/v1/evaluations/providers" with body:
    """
    {
      "name": "test-provider-3",
      "description": "Test provider 3 for FVT",
      "tags": ["test-tag-3", "test-tag-2", "test-tag-1"],
      "benchmarks": [
        {
            "id": "arc_easy",
            "name": "lm_evaluation_harness"
        }
      ]
    }
    """
    Then the response code should be 201
    When I send a GET request to "/api/v1/evaluations/providers?tags=test-tag-1"
    Then the response code should be 200
    And the array at path "items" in the response should have length 3
    When I send a GET request to "/api/v1/evaluations/providers?tags=test-tag-2"
    Then the response code should be 200
    And the array at path "items" in the response should have length 2
    When I send a GET request to "/api/v1/evaluations/providers?tags=test-tag-3"
    Then the response code should be 200
    And the array at path "items" in the response should have length 1
    When I send a GET request to "/api/v1/evaluations/providers?tags=test-tag-4"
    Then the response code should be 200
    And the array at path "items" in the response should have length 0
    When I send a GET request to "/api/v1/evaluations/providers?tags=test-tag-2,test-tag-3"
    Then the response code should be 200
    And the array at path "items" in the response should have length 1
    When I send a GET request to "/api/v1/evaluations/providers?tags=test-tag-2|test-tag-3"
    Then the response code should be 200
    And the array at path "items" in the response should have length 2
    When I send a GET request to "/api/v1/evaluations/providers?tags=test-tag-2%7Ctest-tag-3"
    Then the response code should be 200
    And the array at path "items" in the response should have length 2
    When I send a GET request to "/api/v1/evaluations/providers?name=test-provider-3"
    Then the response code should be 200
    And the array at path "items" in the response should have length 1
    When I send a GET request to "/api/v1/evaluations/providers?name=test-provider-4"
    Then the response code should be 200
    And the array at path "items" in the response should have length 0
    When I send a GET request to "/api/v1/evaluations/providers?name=test-provider-1"
    Then the response code should be 200
    And the array at path "items" in the response should have length 1
    When I send a GET request to "/api/v1/evaluations/providers?name=test-provider-2"
    Then the response code should be 200
    And the array at path "items" in the response should have length 1
    When I send a GET request to "/api/v1/evaluations/providers?name=test-provider-3"
    Then the response code should be 200
    And the array at path "items" in the response should have length 1
    When I send a GET request to "/api/v1/evaluations/providers?name=test-provider-4"
    Then the response code should be 200
    And the array at path "items" in the response should have length 0
    When I send a GET request to "/api/v1/evaluations/providers?name=test-provider-1&tags=test-tag-1"
    Then the response code should be 200
    And the array at path "items" in the response should have length 1
    When I send a GET request to "/api/v1/evaluations/providers?name=test-provider-1&tags=test-tag-2"
    Then the response code should be 200
    And the array at path "items" in the response should have length 1
    When I send a GET request to "/api/v1/evaluations/providers?name=test-provider-1&tags=test-tag-3"
    Then the response code should be 200
    And the array at path "items" in the response should have length 0
    When I send a GET request to "/api/v1/evaluations/providers?name=test-provider-1&tags=test-tag-4"
    Then the response code should be 200
    And the array at path "items" in the response should have length 0
    When I send a GET request to "/api/v1/evaluations/providers?name=test-provider-2&tags=test-tag-1"
    Then the response code should be 200
    And the array at path "items" in the response should have length 1
    When I send a GET request to "/api/v1/evaluations/providers?name=test-provider-2&tags=test-tag-2"
    Then the response code should be 200
    And the array at path "items" in the response should have length 0
    When I send a GET request to "/api/v1/evaluations/providers?name=test-provider-2&tags=test-tag-3"
    Then the response code should be 200
    And the array at path "items" in the response should have length 0
    When I send a GET request to "/api/v1/evaluations/providers?name=test-provider-3&tags=test-tag-1"
    Then the response code should be 200
    And the array at path "items" in the response should have length 1
    When I send a GET request to "/api/v1/evaluations/providers?name=test-provider-3&tags=test-tag-2"
    Then the response code should be 200
    And the array at path "items" in the response should have length 1
    When I send a GET request to "/api/v1/evaluations/providers?name=test-provider-3&tags=test-tag-3"
    Then the response code should be 200
    And the array at path "items" in the response should have length 1

  Scenario: System provider includes complete agent metadata structure
    Given the service is running
    When I send a GET request to "/api/v1/evaluations/providers/lm_evaluation_harness"
    Then the response code should be 200
    And the response should contain "agent"
    And the response should contain the value "model" at path "agent.target_type"
    And the array at path "agent.evaluates" in the response should have length at least 1
    And the array at path "agent.recommended_when" in the response should have length at least 1
    And the array at path "agent.complements" in the response should have length at least 2

  Scenario: Create user provider with full agent metadata returns all fields
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/providers" with body "file:/user_provider_with_agent_metadata_full.json"
    Then the response code should be 201
    And the response should contain "agent"
    And the response should contain the value "internal domain-specific benchmarks" at path "agent.summary"
    And the response should contain the value "model" at path "agent.target_type"
    And the array at path "agent.evaluates" in the response should have length at least 2
    And the array at path "agent.recommended_when" in the response should have length at least 1
    And the array at path "agent.complements" in the response should have length at least 1
    And the array at path "agent.hints" in the response should have length at least 2
    And the array at path "agent.result_interpretation" in the response should have length at least 2

  Scenario: Create user provider with partial agent metadata
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/providers" with body "file:/user_provider_with_agent_metadata_partial.json"
    Then the response code should be 201
    And the response should contain "agent"
    And the array at path "agent.evaluates" in the response should have length at least 2

  Scenario: Update user provider to PUT agent metadata
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/providers" with body "file:/user_provider.json"
    Then the response code should be 201
    When I send a PUT request to "/api/v1/evaluations/providers/{id}" with body "file:/user_provider_with_agent_metadata_partial.json"
    Then the response code should be 200
    And the array at path "agent.evaluates" in the response should have length at least 2

  Scenario: Update user provider to modify agent metadata
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/providers" with body "file:/user_provider_with_agent_metadata_partial.json"
    Then the response code should be 201
    When I send a PUT request to "/api/v1/evaluations/providers/{id}" with body:
      """
      {
        "name": "my-custom-evaluator-updated",
        "title": "Updated title",
        "description": "Internal evaluation adapter for domain-specific benchmarks - Updated",
        "tags": [
          "updated"
        ],
        "agent": {
          "evaluates": [
            "logic_reasoning",
            "language_modeling",
            "science",
            "safety",
            "multilingual",
            "code",
            "medical"
          ]
        },
        "benchmarks": [
          {
            "id": "custom_domain_test-updated",
            "name": "Domain-Specific Test-updated"
          }
        ]
      }
      """
    Then the response code should be 200
    And the array at path "agent.evaluates" in the response should have length at least 5
    And the response should contain the value "my-custom-evaluator-updated" at path "name"
    And the response should contain the value "Updated title" at path "title"

  Scenario: Patch user provider agent metadata - remove agent
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/providers" with body "file:/user_provider_with_agent_metadata_partial.json"
    Then the response code should be 201
    When I send a PATCH request to "/api/v1/evaluations/providers/{id}" with body:
      """
      [
        {
          "op": "remove",
          "path": "/agent"
        }
      ]
      """
    Then the response code should be 200
    And the response should not contain the value "model" at path "agent.target_type"

  @negative
  Scenario: Create provider with invalid target_type
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/providers" with body:
      """
      {
        "name": "my-custom-evaluator-invalid-target-type",
        "agent": {
          "evaluates": [
            "accuracy"
          ],
          "target_type": "invalid_model"
        },
        "benchmarks": [
          {
            "id": "custom_domain_test",
            "name": "Domain-Specific Test"
          }
        ]
      }
      """
    Then the response code should be 400
    And the response should contain the value "request_validation_failed" at path "$.message_code"

  @negative
  Scenario: Create provider with summary too long
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/providers" with body:
      """
      {
        "name": "my-custom-evaluator-summary-too-long",
        "title": "Custom Internal Evaluator",
        "description": "Internal evaluation adapter for domain-specific benchmarks",
        "tags": [
          "custom",
          "internal"
        ],
        "agent": {
          "evaluates": [
            "accuracy"
          ],
          "target_type": "model",
          "summary": "This is a test summary that exceeds the maximum allowed length of 200 characters for the summary field. The validation should reject this request because it contains more than 200 characters in total. Extra padding text here."
        },
        "benchmarks": [
          {
            "id": "custom_domain_test",
            "name": "Domain-Specific Test"
          }
        ]
      }
      """
    Then the response code should be 400
    And the response should contain the value "request_validation_failed" at path "$.message_code"

  Scenario: System provider includes benchmark agent metadata
    Given the service is running
    When I send a GET request to "/api/v1/evaluations/providers/lm_evaluation_harness"
    Then the response code should be 200
    And the array at path "benchmarks[0].agent.score_ranges" in the response should have length at least 2

  Scenario: Create provider with benchmark agent metadata
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/providers" with body:
      """
      {
        "name": "my-custom-evaluator-with-benchmark-metadata",
        "title": "Custom Internal Evaluator",
        "description": "Internal evaluation adapter for domain-specific benchmarks",
        "tags": [
          "custom",
          "internal"
        ],
        "benchmarks": [
          {
            "id": "custom_domain_test",
            "name": "Domain-Specific Test",
            "description": "Custom benchmark for internal domain testing",
            "category": "reasoning",
            "metrics": [
              "acc",
              "f1"
            ],
            "num_few_shot": 0,
            "dataset_size": 1000,
            "tags": [
              "custom",
              "domain"
            ],
            "primary_score": {
              "metric": "acc"
            },
            "pass_criteria": {
              "threshold": 0.5
            },
            "agent": {
              "result_interpretation": "Accuracy from 0-1; higher is better for domain tasks",
              "score_ranges": [
                {
                  "range": "0.0-0.5",
                  "meaning": "Below acceptable threshold"
                },
                {
                  "range": "0.5-0.75",
                  "meaning": "Meets minimum requirements"
                },
                {
                  "range": "0.75-1.0",
                  "meaning": "Excellent domain alignment"
                }
              ]
            }
          }
        ]
      }
      """
    Then the response code should be 201
    And the response should contain the value "Accuracy from 0-1" at path "benchmarks[0].agent.result_interpretation"
    And the array at path "benchmarks[0].agent.score_ranges" in the response should have length at least 2

  @negative
  Scenario: Create benchmark with score_range missing meaning field
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/providers" with body:
      """
      {
        "name": "my-custom-evaluator-missing-meaning",
        "benchmarks": [
          {
            "id": "custom_domain_test",
            "metrics": [
              "acc",
              "f1"
            ],
            "agent": {
              "result_interpretation": "Accuracy from 0-1; higher is better for domain tasks",
              "score_ranges": [
                {
                  "range": "0.0-0.5"
                }
              ]
            }
          }
        ]
      }
      """
    Then the response code should be 400
    And the response should contain the value "request_validation_failed" at path "$.message_code"

  @negative
  Scenario: Create benchmark with score_range missing range field
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/providers" with body:
      """
      {
        "name": "my-custom-evaluator-missing-range",
        "benchmarks": [
          {
            "id": "custom_domain_test",
            "metrics": [
              "acc",
              "f1"
            ],
            "agent": {
              "result_interpretation": "Accuracy from 0-1; higher is better for domain tasks",
              "score_ranges": [
                {
                  "meaning": "Excellent domain alignment"
                }
              ]
            }
          }
        ]
      }
      """
    Then the response code should be 400
    And the response should contain the value "request_validation_failed" at path "$.message_code"

  Scenario: Create provider with target_type agent
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/providers" with body:
      """
      {
        "name": "test-agent-provider",
        "title": "Test Agent Provider",
        "description": "Provider for testing agent target_type",
        "agent": {
          "evaluates": ["reasoning", "planning"],
          "target_type": "agent",
          "summary": "Evaluates AI agent capabilities"
        },
        "benchmarks": [
          {
            "id": "test_agent_benchmark",
            "name": "Agent Test Benchmark"
          }
        ]
      }
      """
    Then the response code should be 201
    And the response should contain the value "agent" at path "agent.target_type"

  Scenario: Create provider with target_type inference_server
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/providers" with body:
      """
      {
        "name": "test-inference-server-provider",
        "title": "Test Inference Server Provider",
        "description": "Provider for testing inference_server target_type",
        "agent": {
          "evaluates": ["throughput", "latency"],
          "target_type": "inference_server",
          "summary": "Evaluates inference server performance"
        },
        "benchmarks": [
          {
            "id": "test_server_benchmark",
            "name": "Server Performance Benchmark"
          }
        ]
      }
      """
    Then the response code should be 201
    And the response should contain the value "inference_server" at path "agent.target_type"
