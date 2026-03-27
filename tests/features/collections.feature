@collections
Feature: Collections Endpoint
  As a data scientist
  I want to create collections of benchmarks
  So that I evaluate models on these collections

  Background:
    Given I set the header "X-Tenant" to "{{env:X_TENANT|test-tenant}}"

  Scenario: Create a collection of benchmarks and get by id
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/collections" with body "file:/collection.json"
    Then the response code should be 201
    When I send a GET request to "/api/v1/evaluations/collections/{id}"
    Then the response code should be 200
    And the response should contain "resource"
    And the response should contain "name"
    And the response should contain "benchmarks"
    And the array at path "benchmarks" in the response should have length 1

  Scenario: Create a collection without benchmarks field returns 400
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/collections" with body "file:/collection_no_benchmarks.json"
    Then the response code should be 400

  Scenario: Create a collection with empty benchmarks array returns 400
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/collections" with body "file:/collection_empty_benchmarks.json"
    Then the response code should be 400

  Scenario: Create a collection without name field returns 400
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/collections" with body "file:/collection_no_name.json"
    Then the response code should be 400

  Scenario: Create a collection without category field returns 400
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/collections" with body "file:/collection_no_category.json"
    Then the response code should be 400

  Scenario: Create a collection without description field returns 201
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/collections" with body "file:/collection_no_description.json"
    Then the response code should be 201

  Scenario: Create a collection with a benchmark that does not contain 'id' returns 400
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/collections" with body "file:/collection_benchmark_no_id.json"
    Then the response code should be 400

  Scenario: Create a collection with a benchmark that does not contain 'provider_id' returns 400
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/collections" with body "file:/collection_benchmark_no_provider_id.json"
    Then the response code should be 400

  # GET collection by id - negative cases (per OpenAPI: 404)
  Scenario: Get collection by non-existent id returns 404
    Given the service is running
    When I send a GET request to "/api/v1/evaluations/collections/00000000-0000-0000-0000-000000000000"
    Then the response code should be 404

  Scenario: Get collection with empty id returns 404
    Given the service is running
    When I send a GET request to "/api/v1/evaluations/collections/"
    Then the response code should be 404

  Scenario: Update collection returns 200 and changes are persisted
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/collections" with body "file:/collection.json"
    Then the response code should be 201
    When I send a PUT request to "/api/v1/evaluations/collections/{id}" with body "file:/collection_update.json"
    Then the response code should be 200
    And the response should contain "name" with value "updated-collection-name"
    And the response should contain "description" with value "Updated description for FVT"
    When I send a GET request to "/api/v1/evaluations/collections/{id}"
    Then the response code should be 200
    And the response should contain "name" with value "updated-collection-name"
    And the response should contain "description" with value "Updated description for FVT"

  Scenario: Update collection with non-existent id returns 404
    Given the service is running
    When I send a PUT request to "/api/v1/evaluations/collections/00000000-0000-0000-0000-000000000000" with body "file:/collection_update.json"
    Then the response code should be 404

  Scenario: Update collection with empty id returns 404
    Given the service is running
    When I send a PUT request to "/api/v1/evaluations/collections/" with body "file:/collection_update.json"
    Then the response code should be 404

  Scenario: Update collection without name in body returns 400
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/collections" with body "file:/collection.json"
    Then the response code should be 201
    When I send a PUT request to "/api/v1/evaluations/collections/{id}" with body "file:/collection_no_name.json"
    Then the response code should be 400

  Scenario: Update collection without description in body returns 200
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/collections" with body "file:/collection.json"
    Then the response code should be 201
    When I send a PUT request to "/api/v1/evaluations/collections/{id}" with body "file:/collection_no_description.json"
    Then the response code should be 200

  Scenario: Update collection without benchmarks in body returns 400
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/collections" with body "file:/collection.json"
    Then the response code should be 201
    When I send a PUT request to "/api/v1/evaluations/collections/{id}" with body "file:/collection_no_benchmarks.json"
    Then the response code should be 400

  Scenario: Update collection with empty benchmarks in body returns 400
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/collections" with body "file:/collection.json"
    Then the response code should be 201
    When I send a PUT request to "/api/v1/evaluations/collections/{id}" with body "file:/collection_empty_benchmarks.json"
    Then the response code should be 400

  Scenario: Update collection with benchmark missing id in body returns 400
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/collections" with body "file:/collection.json"
    Then the response code should be 201
    When I send a PUT request to "/api/v1/evaluations/collections/{id}" with body "file:/collection_benchmark_no_id.json"
    Then the response code should be 400

  Scenario: Update collection with benchmark missing provider_id in body returns 400
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/collections" with body "file:/collection.json"
    Then the response code should be 201
    When I send a PUT request to "/api/v1/evaluations/collections/{id}" with body "file:/collection_benchmark_no_provider_id.json"
    Then the response code should be 400

  # Patch collection (per OpenAPI: PATCH .../collections/{id}, body = array of PatchOperation, 200/400/404)
  Scenario: Patch collection returns 200 and changes are persisted
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/collections" with body "file:/collection.json"
    Then the response code should be 201
    When I send a PATCH request to "/api/v1/evaluations/collections/{id}" with body "file:/patch_collection_name.json"
    Then the response code should be 200
    When I send a GET request to "/api/v1/evaluations/collections/{id}"
    Then the response code should be 200
    And the response should contain "name" with value "patched-collection-name"

  Scenario: Patch a benchmark element in collection returns 200 and change is persisted
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/collections" with body "file:/collection.json"
    Then the response code should be 201
    When I send a PATCH request to "/api/v1/evaluations/collections/{id}" with body "file:/patch_collection_benchmark.json"
    Then the response code should be 200
    When I send a GET request to "/api/v1/evaluations/collections/{id}"
    Then the response code should be 200
    And the response should contain the value "patched-benchmark-id" at path "benchmarks[0].id"
    And the array at path "benchmarks" in the response should have length 1

  Scenario: Patch entire benchmark element in collection returns 200 and change is persisted
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/collections" with body "file:/collection.json"
    Then the response code should be 201
    When I send a PATCH request to "/api/v1/evaluations/collections/{id}" with body "file:/patch_collection_benchmark_full.json"
    Then the response code should be 200
    When I send a GET request to "/api/v1/evaluations/collections/{id}"
    Then the response code should be 200
    And the response should contain the value "replaced-benchmark-id" at path "benchmarks[0].id"
    And the response should contain the value "other_provider" at path "benchmarks[0].provider_id"
    And the array at path "benchmarks" in the response should have length 1

  Scenario: Patch collection with non-existent id returns 404
    Given the service is running
    When I send a PATCH request to "/api/v1/evaluations/collections/00000000-0000-0000-0000-000000000000" with body "file:/patch_collection_name.json"
    Then the response code should be 404

  Scenario: Patch collection with empty id returns 404
    Given the service is running
    When I send a PATCH request to "/api/v1/evaluations/collections/" with body "file:/patch_collection_name.json"
    Then the response code should be 404

  Scenario: Patch collection with invalid body returns 400
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/collections" with body "file:/collection.json"
    Then the response code should be 201
    When I send a PATCH request to "/api/v1/evaluations/collections/{id}" with body "file:/patch_collection_invalid.json"
    Then the response code should be 400

  # List collections - positive and negative (per OpenAPI: 200, 400, 404)
  Scenario: List collections returns 200
    Given the service is running
    When I send a GET request to "/api/v1/evaluations/collections"
    Then the response code should be 200
    And the response should contain "items"
    And the response should contain "limit"
    And the response should contain "total_count"

  Scenario: List collections with pagination params returns 200
    Given the service is running
    When I send a GET request to "/api/v1/evaluations/collections?limit=5&offset=0"
    Then the response code should be 200
    And the response should contain "items"
    And the response should contain "limit"
    And the response should contain "total_count"

  @benchmark_url_custom_provider
  Scenario: List and get collection include benchmark url from custom provider
    Given the service is running
    And I set the header "X-Tenant" to "test-tenant"
    When I send a POST request to "/api/v1/evaluations/providers" with body "file:/user_provider_benchmark_url.json"
    Then the response code should be 201
    And the "resource.id" field in the response should be saved as "value:custom_provider_id"
    When I send a POST request to "/api/v1/evaluations/collections" with body "file:/collection_custom_provider_benchmark_url.json"
    Then the response code should be 201
    When I send a GET request to "/api/v1/evaluations/collections?name=fvt-benchmark-url-collection"
    Then the response code should be 200
    And the array at path "items" in the response should have length 1
    And the response should contain the value "https://example.com/fvt-custom-provider-benchmark" at path "$.items[0].benchmarks[0].url"
    When I send a GET request to "/api/v1/evaluations/collections/{id}"
    Then the response code should be 200
    And the response should contain the value "https://example.com/fvt-custom-provider-benchmark" at path "$.benchmarks[0].url"

  Scenario: List collections pagination returns next href and next page contains remaining item
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/collections" with body "file:/collection.json"
    Then the response code should be 201
    And the "resource.id" field in the response should be saved as "value:first_id"
    When I send a POST request to "/api/v1/evaluations/collections" with body "file:/collection.json"
    Then the response code should be 201
    And the "resource.id" field in the response should be saved as "value:second_id"
    When I send a POST request to "/api/v1/evaluations/collections" with body "file:/collection.json"
    Then the response code should be 201
    When I send a GET request to "/api/v1/evaluations/collections?limit=2&offset=0"
    Then the response code should be 200
    And the array at path "items" in the response should have length 2
    And the response should contain "next"
    And the "next.href" field in the response should be saved as "value:next_url"
    When I send a GET request to "{{value:next_url}}"
    Then the response code should be 200
    And the array at path "items" in the response should have length at least 1
    When I send a DELETE request to "/api/v1/evaluations/collections/{{value:first_id}}?hard_delete=true"
    Then the response code should be 204
    When I send a DELETE request to "/api/v1/evaluations/collections/{{value:second_id}}?hard_delete=true"
    Then the response code should be 204
    When I send a DELETE request to "/api/v1/evaluations/collections/{id}?hard_delete=true"
    Then the response code should be 204

  Scenario: List collections with invalid limit returns 400
    Given the service is running
    When I send a GET request to "/api/v1/evaluations/collections?limit=invalid"
    Then the response code should be 400

  Scenario: List collections with invalid offset returns 400
    Given the service is running
    When I send a GET request to "/api/v1/evaluations/collections?offset=not-a-number"
    Then the response code should be 400

  Scenario: List collections with invalid scope returns 400
    Given the service is running
    When I send a GET request to "/api/v1/evaluations/collections?scope=invalid"
    Then the response code should be 400
    And the response should contain the value "query_parameter_value_invalid" at path "$.message_code"

  Scenario: Delete collection with non-existent id returns 404
    Given the service is running
    When I send a DELETE request to "/api/v1/evaluations/collections/00000000-0000-0000-0000-000000000000?hard_delete=true"
    Then the response code should be 404
    And the response should contain the value "resource_not_found" at path "$.message_code"

  Scenario: List collections by tags and name and category
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/collections" with body:
    """
    {
      "name": "test-collection-1",
      "description": "Collection of benchmarks for FVT",
      "category": "test",
      "tags": ["test-tag-1", "test-tag-2"],
      "benchmarks": [
        {
          "id": "arc_easy",
          "provider_id": "lm_evaluation_harness"
        }
      ]
    }
    """
    Then the response code should be 201
    And the "resource.id" field in the response should be saved as "value:first_id"
    When I send a POST request to "/api/v1/evaluations/collections" with body:
    """
    {
      "name": "test-collection-2",
      "description": "Collection of benchmarks for FVT",
      "category": "test",
      "tags": ["test-tag-1"],
      "benchmarks": [
        {
          "id": "arc_easy",
          "provider_id": "lm_evaluation_harness"
        }
      ]
    }
    """
    Then the response code should be 201
    And the "resource.id" field in the response should be saved as "value:second_id"
    When I send a POST request to "/api/v1/evaluations/collections" with body:
    """
    {
      "name": "test-collection-3",
      "description": "Collection of benchmarks for FVT",
      "category": "test3",
      "tags": ["test-tag-3", "test-tag-2", "test-tag-1"],
      "benchmarks": [
        {
          "id": "arc_easy",
          "provider_id": "lm_evaluation_harness"
        }
      ]
    }
    """
    Then the response code should be 201
    When I send a GET request to "/api/v1/evaluations/collections?tags=test-tag-1"
    Then the response code should be 200
    And the array at path "items" in the response should have length 3
    When I send a GET request to "/api/v1/evaluations/collections?tags=test-tag-2"
    Then the response code should be 200
    And the array at path "items" in the response should have length 2
    When I send a GET request to "/api/v1/evaluations/collections?tags=test-tag-3"
    Then the response code should be 200
    And the array at path "items" in the response should have length 1
    When I send a GET request to "/api/v1/evaluations/collections?tags=test-tag-4"
    Then the response code should be 200
    And the array at path "items" in the response should have length 0
    When I send a GET request to "/api/v1/evaluations/collections?tags=test-tag-2,test-tag-3"
    Then the response code should be 200
    And the array at path "items" in the response should have length 1
    When I send a GET request to "/api/v1/evaluations/collections?tags=test-tag-2|test-tag-3"
    Then the response code should be 200
    And the array at path "items" in the response should have length 2
    When I send a GET request to "/api/v1/evaluations/collections?tags=test-tag-2%7Ctest-tag-3"
    Then the response code should be 200
    And the array at path "items" in the response should have length 2
    When I send a GET request to "/api/v1/evaluations/collections?name=test-collection-3"
    Then the response code should be 200
    And the array at path "items" in the response should have length 1
    When I send a GET request to "/api/v1/evaluations/collections?name=test-collection-4"
    Then the response code should be 200
    And the array at path "items" in the response should have length 0
    When I send a GET request to "/api/v1/evaluations/collections?name=test-collection-1"
    Then the response code should be 200
    And the array at path "items" in the response should have length 1
    When I send a GET request to "/api/v1/evaluations/collections?name=test-collection-2"
    Then the response code should be 200
    And the array at path "items" in the response should have length 1
    When I send a GET request to "/api/v1/evaluations/collections?name=test-collection-3"
    Then the response code should be 200
    And the array at path "items" in the response should have length 1
    When I send a GET request to "/api/v1/evaluations/collections?name=test-collection-4"
    Then the response code should be 200
    And the array at path "items" in the response should have length 0
    When I send a GET request to "/api/v1/evaluations/collections?name=test-collection-1&tags=test-tag-1"
    Then the response code should be 200
    And the array at path "items" in the response should have length 1
    When I send a GET request to "/api/v1/evaluations/collections?name=test-collection-1&tags=test-tag-2"
    Then the response code should be 200
    And the array at path "items" in the response should have length 1
    When I send a GET request to "/api/v1/evaluations/collections?name=test-collection-1&tags=test-tag-3"
    Then the response code should be 200
    And the array at path "items" in the response should have length 0
    When I send a GET request to "/api/v1/evaluations/collections?name=test-collection-1&tags=test-tag-4"
    Then the response code should be 200
    And the array at path "items" in the response should have length 0
    When I send a GET request to "/api/v1/evaluations/collections?name=test-collection-2&tags=test-tag-1"
    Then the response code should be 200
    And the array at path "items" in the response should have length 1
    When I send a GET request to "/api/v1/evaluations/collections?name=test-collection-2&tags=test-tag-2"
    Then the response code should be 200
    And the array at path "items" in the response should have length 0
    When I send a GET request to "/api/v1/evaluations/collections?name=test-collection-2&tags=test-tag-3"
    Then the response code should be 200
    And the array at path "items" in the response should have length 0
    When I send a GET request to "/api/v1/evaluations/collections?name=test-collection-3&tags=test-tag-1"
    Then the response code should be 200
    And the array at path "items" in the response should have length 1
    When I send a GET request to "/api/v1/evaluations/collections?name=test-collection-3&tags=test-tag-2"
    Then the response code should be 200
    And the array at path "items" in the response should have length 1
    When I send a GET request to "/api/v1/evaluations/collections?name=test-collection-3&tags=test-tag-3"
    Then the response code should be 200
    And the array at path "items" in the response should have length 1
    When I send a GET request to "/api/v1/evaluations/collections?category=test"
    Then the response code should be 200
    And the array at path "items" in the response should have length 2
    When I send a GET request to "/api/v1/evaluations/collections?category=test3"
    Then the response code should be 200
    And the array at path "items" in the response should have length 1
    When I send a GET request to "/api/v1/evaluations/collections?category=test4"
    Then the response code should be 200
    And the array at path "items" in the response should have length 0
    When I send a GET request to "/api/v1/evaluations/collections?category=test&name=test-collection-1"
    Then the response code should be 200
    And the array at path "items" in the response should have length 1
    When I send a GET request to "/api/v1/evaluations/collections?category=test&name=test-collection-2"
    Then the response code should be 200
    And the array at path "items" in the response should have length 1
    When I send a GET request to "/api/v1/evaluations/collections?category=test&name=test-collection-3"
    Then the response code should be 200
    And the array at path "items" in the response should have length 0
    When I send a GET request to "/api/v1/evaluations/collections?category=test3&name=test-collection-3"
    Then the response code should be 200
    And the array at path "items" in the response should have length 1
    When I send a GET request to "/api/v1/evaluations/collections?category=test&name=test-collection-4"
    Then the response code should be 200
    And the array at path "items" in the response should have length 0

  Scenario: List system defined collections with pagination
    Given the service is running
    And there are no user collections
    When I send a GET request to "/api/v1/evaluations/collections?limit=50&offset=0&scope=system"
    Then the response code should be 200
    And the response should contain the value "50" at path "$.limit"
    And the "total_count" field in the response should be saved as "value:num_collections"
    And the response should contain the value "1" at path "$.total_count"
    When I send a GET request to "/api/v1/evaluations/collections?limit=50&offset=0"
    Then the response code should be 200
    And the response should contain the value "50" at path "$.limit"
    And the array at path "items" in the response should have length "value:num_collections"
    And the response should contain the value "{{value:num_collections}}" at path "$.total_count"
