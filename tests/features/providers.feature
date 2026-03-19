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

  Scenario: List providers with scope=tenant returns only user providers
    Given the service is running
    And there are no user providers
    And I set the header "X-Tenant" to "test-tenant"
    When I send a POST request to "/api/v1/evaluations/providers" with body "file:/user_provider.json"
    Then the response code should be 201
    When I send a GET request to "/api/v1/evaluations/providers?scope=tenant"
    Then the response code should be 200
    And the array at path "items" in the response should have length 1
    And the response should contain the value "Test Provider" at path "items[0].name"
    When I send a DELETE request to "/api/v1/evaluations/providers/{id}"
    Then the response code should be 204

  Scenario: List providers with scope=system returns only system providers
    Given the service is running
    And there are system providers
    And I set the header "X-Tenant" to "test-tenant"
    When I send a GET request to "/api/v1/evaluations/providers?scope=system"
    Then the response code should be 200
    And the array at path "items" in the response should have length at least 1
    And the response should contain the value "system" at path "items[0].resource.owner"

  Scenario: List providers with no returns all providers
    Given the service is running
    And there are system providers
    And there are no user providers
    And I set the header "X-Tenant" to "test-tenant"
    When I send a POST request to "/api/v1/evaluations/providers" with body "file:/user_provider.json"
    Then the response code should be 201
    When I send a POST request to "/api/v1/evaluations/providers" with body "file:/user_provider.json"
    Then the response code should be 201
    When I send a GET request to "/api/v1/evaluations/providers"
    Then the response code should be 200
    And the response should contain at least the value "3" at path "total_count"
    And the array at path "items" in the response should have length at least 3

  Scenario: List providers with invalid limit returns 400
    Given the service is running
    When I send a GET request to "/api/v1/evaluations/providers?limit=-1"
    Then the response code should be 400
    And the response should contain the value "query_parameter_invalid" at path "message_code"

  Scenario: List system providers with pagination
    Given the service is running
    And there are system providers
    # This will skip the scenario if there are no system providers
    When I send a GET request to "/api/v1/evaluations/providers?limit=50&offset=0&scope=system"
    Then the response code should be 200
    And the response should contain the value "50" at path "$.limit"
    And the "total_count" field in the response should be saved as "value:num_providers"
    And the response should contain the value "3" at path "$.total_count"
    When I send a GET request to "/api/v1/evaluations/providers?limit=50&offset=0"
    Then the response code should be 200
    And the response should contain the value "50" at path "$.limit"
    And the array at path "items" in the response should have length "value:num_providers"
    And the response should contain the value "{{value:num_providers}}" at path "$.total_count"
    When I send a GET request to "/api/v1/evaluations/providers?limit=50&offset=1"
    Then the response code should be 200
    And the response should contain the value "50" at path "$.limit"
    And the array at path "items" in the response should have length 2
    And the response should contain the value "{{value:num_providers}}" at path "$.total_count"
    When I send a GET request to "/api/v1/evaluations/providers?limit=50&offset=2"
    Then the response code should be 200
    And the response should contain the value "50" at path "$.limit"
    And the array at path "items" in the response should have length 1
    And the response should contain the value "{{value:num_providers}}" at path "$.total_count"
    When I send a GET request to "/api/v1/evaluations/providers?limit=50&offset=3"
    Then the response code should be 200
    And the response should contain the value "50" at path "$.limit"
    And the array at path "items" in the response should have length 0
    And the response should contain the value "{{value:num_providers}}" at path "$.total_count"
    When I send a POST request to "/api/v1/evaluations/providers" with body "file:/user_provider.json"
    Then the response code should be 201
    And the "resource.id" field in the response should be saved as "value:provider1_id"
    When I send a GET request to "/api/v1/evaluations/providers?limit=50&offset=0&scope=system"
    Then the response code should be 200
    And the response should contain the value "{{value:num_providers}}" at path "$.total_count"
    When I send a GET request to "/api/v1/evaluations/providers?limit=50&offset={{value:num_providers}}"
    Then the response code should be 200
    And the response should contain the value "50" at path "$.limit"
    And the array at path "items" in the response should have length 1
    When I send a POST request to "/api/v1/evaluations/providers" with body "file:/user_provider.json"
    Then the response code should be 201
    And the "resource.id" field in the response should be saved as "value:provider2_id"
    When I send a GET request to "/api/v1/evaluations/providers?limit=50&offset=3"
    Then the response code should be 200
    And the response should contain the value "50" at path "$.limit"
    And the array at path "items" in the response should have length 2
    When I send a GET request to "/api/v1/evaluations/providers?limit=1&offset=3"
    Then the response code should be 200
    And the response should contain the value "1" at path "$.limit"
    And the array at path "items" in the response should have length 1
    And the response should contain the value "{{value:provider1_id}}|{{value:provider2_id}}" at path "$.items[0].resource.id"

  Scenario: List providers with all search parameters and pagination
    Given the service is running
    And I set the header "X-Tenant" to "test-tenant"
    And there are no user providers
    When I send a POST request to "/api/v1/evaluations/providers" with body "file:/user_provider.json"
    Then the response code should be 201
    And the "resource.id" field in the response should be saved as "value:provider1_id"
    When I send a POST request to "/api/v1/evaluations/providers" with body "file:/user_provider_with_tags.json"
    Then the response code should be 201
    And the "resource.id" field in the response should be saved as "value:provider2_id"
    When I send a GET request to "/api/v1/evaluations/providers?limit=2&offset=0&scope=tenant"
    Then the response code should be 200
    And the response should contain the value "2" at path "$.limit"
    And the array at path "items" in the response should have length 2
    And the response should contain the value "2" at path "$.total_count"
    And the response should not contain the value "next" at path "$.next"
    When I send a GET request to "/api/v1/evaluations/providers?limit=5&offset=0"
    Then the response code should be 200
    And the response should contain the value "5" at path "$.limit"
    And the array at path "items" in the response should have length at least 4
    When I send a GET request to "/api/v1/evaluations/providers?limit=2&offset=1&scope=tenant"
    Then the response code should be 200
    And the response should contain the value "2" at path "$.limit"
    And the array at path "items" in the response should have length at least 1
    When I send a GET request to "/api/v1/evaluations/providers?limit=100&offset=0"
    Then the response code should be 200
    And the response should contain the value "100" at path "$.limit"
    When I send a GET request to "/api/v1/evaluations/providers?benchmarks=false&limit=5"
    Then the response code should be 200
    And the response should not contain the value "0" at path "$.total_count"
    And the response should contain the value "[]" at path "items[0].benchmarks"
    When I send a GET request to "/api/v1/evaluations/providers?tags=nonexistent-tag&limit=10"
    Then the response code should be 200
    And the response should contain the value "0" at path "$.total_count"
    When I send a GET request to "/api/v1/evaluations/providers?scope=tenant&limit=10"
    Then the response code should be 200
    And the array at path "items" in the response should have length 2
    And the response should contain the value "2" at path "$.total_count"
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
    And there are no user providers
    And I set the header "X-User" to "prov-owner-a"
    And I set the header "X-Tenant" to "prov-tenant-x"
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
    And the response should contain the value "3" at path "$.total_count"
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
    And the response should contain the value "2" at path "$.limit"
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
    And the response should not contain the value "0" at path "$.total_count"
    And the response should contain the value "[]" at path "items[0].benchmarks"
    When I send a GET request to "/api/v1/evaluations/providers?benchmarks=true&limit=5"
    Then the response code should be 200
    And the array at path "items[0].benchmarks" in the response should have length at least 1
    When I send a GET request to "/api/v1/evaluations/providers?limit=2&name=Test%20Provider&benchmarks=false"
    Then the response code should be 200
    And the response should contain the value "2" at path "$.limit"
    And the array at path "items" in the response should have length at least 1
    And the response should not contain the value "0" at path "$.total_count"
    And the response should contain the value "[]" at path "items[0].benchmarks"
    And the response should contain the value "Test Provider" at path "$.items[0].name"
    When I send a DELETE request to "/api/v1/evaluations/providers/{{value:list_prov1_id}}"
    Then the response code should be 204
    When I send a DELETE request to "/api/v1/evaluations/providers/{{value:list_prov2_id}}"
    Then the response code should be 204
    When I send a DELETE request to "/api/v1/evaluations/providers/{{value:list_prov3_id}}"
    Then the response code should be 204

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
    And the response should not contain the value "0" at path "$.total_count"
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

  Scenario: Update system provider returns 400
    Given the service is running
    When I send a PUT request to "/api/v1/evaluations/providers/lm_evaluation_harness" with body "file:/user_provider_update.json"
    Then the response code should be 400
    And the response should contain the value "read_only_provider" at path "message_code"

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

  Scenario: Patch system provider returns 400
    Given the service is running
    When I send a PATCH request to "/api/v1/evaluations/providers/lm_evaluation_harness" with body "file:/user_provider_patch.json"
    Then the response code should be 400
    And the response should contain the value "read_only_provider" at path "message_code"

  Scenario: Update non-existent provider returns 404
    Given the service is running
    When I send a PUT request to "/api/v1/evaluations/providers/00000000-0000-0000-0000-000000000000" with body "file:/user_provider_update.json"
    Then the response code should be 404

  Scenario: Patch non-existent provider returns 404
    Given the service is running
    When I send a PATCH request to "/api/v1/evaluations/providers/00000000-0000-0000-0000-000000000000" with body "file:/user_provider_patch.json"
    Then the response code should be 404

  Scenario: Update provider with empty path returns 404
    Given the service is running
    When I send a PUT request to "/api/v1/evaluations/providers/" with body "file:/user_provider_update.json"
    Then the response code should be 404

  Scenario: Get provider with empty path returns 404
    Given the service is running
    When I send a GET request to "/api/v1/evaluations/providers/"
    Then the response code should be 404

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
