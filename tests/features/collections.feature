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
    And the response should contain the value "3" at path "benchmarks[0].parameters.weight"

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
  Scenario: Create collection persists benchmark url; list and get return stored url
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/providers" with body "file:/user_provider_benchmark_url.json"
    Then the response code should be 201
    And the "resource.id" field in the response should be saved as "value:custom_provider_id"
    When I send a POST request to "/api/v1/evaluations/collections" with body "file:/collection_custom_provider_benchmark_url.json"
    Then the response code should be 201
    And the response should contain the value "https://example.com/fvt-custom-provider-benchmark" at path "$.benchmarks[0].url"
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
    When I send a GET request to "/api/v1/evaluations/collections?limit=50&offset=0&scope=system"
    Then the response code should be 200
    And the response should contain the value "50" at path "$.limit"
    And the "total_count" field in the response should be saved as "value:num_collections"
    When I send a GET request to "/api/v1/evaluations/collections?limit={{value:num_collections}}&offset=0"
    Then the response code should be 200
    And the response should contain the value "{{value:num_collections}}" at path "$.limit"
    And the array at path "items" in the response should have length "{{value:num_collections}}"
    And the response should contain at least the value "{{value:num_collections}}" at path "$.total_count"

  Scenario: Create threshold-zero collection
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/collections" with body:
    """
    {
        "name": "test-benchmarks-collection-threshold-zero",
        "category": "test",
        "description": "Collection of benchmarks for FVT",
        "pass_criteria": {
            "threshold": 0
        },
        "benchmarks": [
            {
                "id": "arc_easy",
                "provider_id": "lm_evaluation_harness",
                "primary_score": {
                    "metric": "acc_norm",
                    "lower_is_better": false
                },
                "pass_criteria": {
                    "threshold": 0.5
                },
                "parameters": {
                    "limit": 10,
                    "num_fewshot": 0,
                    "tokenizer": "google/flan-t5-small"
                }
            },
            {
                "id": "arc_easy",
                "provider_id": "lm_evaluation_harness",
                "primary_score": {
                    "metric": "acc_norm",
                    "lower_is_better": false
                },
                "pass_criteria": {
                    "threshold": 0.5
                },
                "parameters": {
                    "limit": 10,
                    "num_fewshot": 0,
                    "tokenizer": "google/flan-t5-small"
                }
            }
        ]
    }
    """
    Then the response code should be 201
    And the "resource.id" field in the response should be saved as "value:collection_id"
    And the response should contain the value "test-benchmarks-collection-threshold-zero" at path "$.name"
    And the response should contain the value "test" at path "$.category"
    And the response should contain the value "Collection of benchmarks for FVT" at path "$.description"
    And the response should contain the value "0" at path "$.pass_criteria.threshold"
    And the array at path "$.benchmarks" in the response should have length 2

  Scenario: Verify soft delete of collection
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/collections" with body "file:/collection.json"
    Then the response code should be 201
    When I send a GET request to "/api/v1/evaluations/collections/{id}"
    Then the response code should be 200
    When I send a DELETE request to "/api/v1/evaluations/collections/{id}"
    Then the response code should be 204
    When I send a GET request to "/api/v1/evaluations/collections/{id}"
    Then the response code should be 404
    When I send a DELETE request to "/api/v1/evaluations/collections/{id}"
    Then the response code should be 404
  
  Scenario: Create collection with weighted benchmarks
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/collections" with body:
      """
      {
        "name": "test-multiple-benchmarks-collection",
        "description": "Collection of benchmarks for FVT",
        "category": "test",
        "benchmarks": [
          {
            "id": "arc_easy",
            "provider_id": "lm_evaluation_harness",
            "parameters": {
              "num_examples": 10,
              "num_fewshot": 3,
              "limit": 5,
              "tokenizer": "google/flan-t5-small",
              "weight": 3
            }
          },
          {
            "id": "arc_easy",
            "provider_id": "lm_evaluation_harness",
            "parameters": {
              "num_examples": 5,
              "tokenizer": "google/flan-t5-small",
              "weight": 2
            }
          }
        ]
      }
      """
    Then the response code should be 201
    And the array at path "$.benchmarks" in the response should have length 2
    And the response should contain the value "3" at path "$.benchmarks[0].parameters.weight"
    And the response should contain the value "2" at path "$.benchmarks[1].parameters.weight"
    When I send a GET request to "/api/v1/evaluations/collections/{id}"
    Then the response code should be 200
    And the response should contain the value "3" at path "$.benchmarks[0].parameters.weight"
    And the response should contain the value "2" at path "$.benchmarks[1].parameters.weight"
    When I send a DELETE request to "/api/v1/evaluations/collections/{id}?hard_delete=true"
    Then the response code should be 204
  
  Scenario: List collections with scope=tenant
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/collections" with body "file:/collection.json"
    Then the response code should be 201
    And the "resource.id" field in the response should be saved as "value:collection_id"
    When I send a GET request to "/api/v1/evaluations/collections?scope=tenant"
    Then the response code should be 200
    And the response should contain the value "{{value:collection_id}}" at path "$.items[0].resource.id"
    And the response should not contain the value "system" at path "$.items[0].resource.owner"
    And the array at path "items" in the response should have length 1
    When I send a GET request to "/api/v1/evaluations/collections?scope=system"
    Then the response code should be 200
    And the response should contain the value "system" at path "$.items[0].resource.owner"
    And the array at path "items" in the response should have length at least 1
    When I send a DELETE request to "/api/v1/evaluations/collections/{{value:collection_id}}?hard_delete=true"
    Then the response code should be 204
  
  Scenario: Verify out of box collection retrieval by id
    Given the service is running
    When I send a GET request to "/api/v1/evaluations/collections/safety-and-fairness-v1"
    Then the response code should be 200
    And the response should contain "name" with value "Safety & Fairness"
    And the response should contain "category" with value "safety"
    And the response should contain "benchmarks"
    And the array at path "benchmarks" in the response should have length 6
    And the response should contain the value "0.758" at path "$.pass_criteria.threshold"
    And the response should contain the value "truthfulqa_mc1" at path "$.benchmarks[0].id"
    And the response should contain the value "toxigen" at path "$.benchmarks[1].id"
    And the response should contain the value "winogender" at path "$.benchmarks[2].id"
    And the response should contain the value "crows_pairs_english" at path "$.benchmarks[3].id"
    And the response should contain the value "bbq" at path "$.benchmarks[4].id"
    And the response should contain the value "ethics_cm" at path "$.benchmarks[5].id"
    And the response should contain the value "2" at path "$.benchmarks[0].weight"
    And the response should contain the value "1" at path "$.benchmarks[3].weight"
    And the response should contain the value "3" at path "$.benchmarks[5].weight"
 
  Scenario: Verify OOB Collections Are Immutable
    Given the service is running
    When I send a DELETE request to "/api/v1/evaluations/collections/leaderboard-v2?hard_delete=true"
    Then the response code should be 400
    And the response should contain the value "read_only_collection" at path "$.message_code"
    And the response should contain the value "Collection 'leaderboard-v2' cannot be modified or deleted" at path "$.message"
  
  Scenario: Verify Evaluation Jobs Can Use OOB Collections
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body:

      """
      {
        "name": "test-evaluation-job-oob-collection",
        "collection": {
          "id": "toxicity-and-ethical-principles"
        },
        "model": {
          "url": "{{env:MODEL_URL|http://test.com}}",
          "name": "{{env:MODEL_NAME|test}}"
        }
      }
      """
    Then the response code should be 202
    And the response should contain the value "toxicity-and-ethical-principles" at path "$.collection.id"