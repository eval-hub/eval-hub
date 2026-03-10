@evaluations
Feature: Evaluations Endpoint
  As a data scientist
  I want to create evaluation jobs
  So that I evaluate models

  Background:
    Given I set the header "X-Tenant" to "test-tenant"

  Scenario: Create an evaluation job
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job.json"
    Then the response code should be 202
    And the response should contain the value "evaluation_job_created" at path "$.status.message.message_code"
    And the response should contain the value "pending" at path "$.status.state"
    And the response should contain the value "{{env:MODEL_NAME|test}}" at path "$.model.name"
    And the response should contain the value "{{env:MODEL_URL|http://test.com}}" at path "$.model.url"
    And the response should contain the value "arc_easy" at path "$.benchmarks[0].id"
    And the response should contain the value "garak|lm_evaluation_harness" at path "$.benchmarks[0].provider_id"
    And the response should contain the value "3" at path "$.benchmarks[0].parameters.num_fewshot"
    And the response should contain the value "5" at path "$.benchmarks[0].parameters.limit"
    And the response should contain the value "google/flan-t5-small" at path "$.benchmarks[0].parameters.tokenizer"
    And the response should contain the value "environment" at path "$.experiment.tags[0].key"
    And the response should contain the value "test" at path "$.experiment.tags[0].value"
    When I send a GET request to "/api/v1/evaluations/jobs/{id}"
    Then the response code should be 200
    And the response should contain the value "pending" at path "$.status.state"
    And the response should contain the value "{{env:MODEL_NAME|test}}" at path "$.model.name"
    And the response should contain the value "{{env:MODEL_URL|http://test.com}}" at path "$.model.url"
    And the response should contain the value "arc_easy" at path "$.benchmarks[0].id"
    And the response should contain the value "garak|lm_evaluation_harness" at path "$.benchmarks[0].provider_id"
    And the response should contain the value "3" at path "$.benchmarks[0].parameters.num_fewshot"
    And the response should contain the value "5" at path "$.benchmarks[0].parameters.limit"
    And the response should contain the value "google/flan-t5-small" at path "$.benchmarks[0].parameters.tokenizer"
    And the response should contain the value "environment" at path "$.experiment.tags[0].key"
    And the response should contain the value "test" at path "$.experiment.tags[0].value"
    And the response should not contain the value "collection" at path "$.collection"
    When I send a DELETE request to "/api/v1/evaluations/jobs/{id}?hard_delete=true"
    Then the response code should be 204
    When I send a GET request to "/api/v1/evaluations/jobs/{id}"
    Then the response code should be 404
    And the response should contain the value "resource_not_found" at path "$.message_code"

  Scenario: Create an evaluation job and wait for completion
    Given the service is running
    When the mode is local or CI then skip this scenario
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job.json"
    Then the response code should be 202
    And I wait for the evaluation job status to be "completed"
    When I send a DELETE request to "/api/v1/evaluations/jobs/{id}?hard_delete=true"
    Then the response code should be 204
    When I send a GET request to "/api/v1/evaluations/jobs/{id}"
    Then the response code should be 404
    And the response should contain the value "resource_not_found" at path "$.message_code"
    When I send a DELETE request to "/api/v1/evaluations/jobs/{id}?hard_delete=true"
    Then the response code should be 404

  Scenario: Create evaluation job missing name
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job_missing_name.json"
    Then the response code should be 400
    And the response should contain the value "request_validation_failed" at path "$.message_code"

  Scenario: Create evaluation job missing model
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job_missing_model.json"
    Then the response code should be 400

  Scenario: Create evaluation job with invalid provider
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job_invalid_provider.json"
    Then the response code should be 400
    And the response should contain the value "request_field_invalid" at path "$.message_code"

  Scenario: Create evaluation job with invalid benchmark
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job_invalid_benchmark.json"
    Then the response code should be 400
    And the response should contain the value "request_field_invalid" at path "$.message_code"

  Scenario: Create evaluation job missing benchmark id
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job_missing_benchmark_id.json"
    Then the response code should be 400
    And the response should contain the value "request_validation_failed" at path "$.message_code"

  Scenario: Create evaluation job missing benchmark provider_id
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job_missing_provider_id.json"
    Then the response code should be 400
    And the response should contain the value "request_validation_failed" at path "$.message_code"

  Scenario: Create evaluation job with Collection
    Given the service is running
    When the mode is local or CI then skip this scenario
    When I send a POST request to "/api/v1/evaluations/collections" with body "file:/collection.json"
    Then the response code should be 202
    And the "resource.id" field in the response should be saved as "value:collection_id"
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job_with_collection.json"
    Then the response code should be 202
    And the response should contain the value "evaluation_job_created" at path "$.status.message.message_code"
    And the response should contain the value "pending" at path "$.status.state"
    When I send a GET request to "/api/v1/evaluations/jobs/{id}"
    Then the response code should be 200
    And the response should contain the value "{{value:collection_id}}" at path "$.collection.id"
    And I wait for the evaluation job status to be "completed"
    When I send a DELETE request to "/api/v1/evaluations/jobs/{id}?hard_delete=true"
    Then the response code should be 204
    When I send a DELETE request to "/api/v1/evaluations/collections/{{value:collection_id}}?hard_delete=true"
    Then the response code should be 204

  Scenario: List evaluation jobs
    Given the service is running
    And I set the header "X-User" to "test-user-1"
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job.json"
    Then the response code should be 202
    And the response should contain the value "test-user-1" at path "$.resource.owner"
    And the response should contain the value "test-tenant" at path "$.resource.tenant"
    And I set the header "X-User" to "test-user-2"
    And I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job.json"
    Then the response code should be 202
    And the response should contain the value "test-user-2" at path "$.resource.owner"
    And the response should contain the value "test-tenant" at path "$.resource.tenant"
    And I set the header "X-User" to "test-user-3"
    And I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job.json"
    Then the response code should be 202
    And the response should contain the value "test-user-3" at path "$.resource.owner"
    And the response should contain the value "test-tenant" at path "$.resource.tenant"
    When I send a GET request to "/api/v1/evaluations/jobs?limit=2"
    Then the response code should be 200
    And the "next.href" field in the response should be saved as "value:next_url"
    And the response should have schema as:
    """
      {
        "properties": {
            "first": {"type": "object"},
            "next": {
              "type": "object",
              "properties": {
                "href": {"type": "string"}
              },
              "required": ["href"]
            },
            "limit": {"type": "integer"},
            "total_count": {
              "type": "integer",
              "minimum": 3
            },
            "items": {
              "type": "array",
              "minItems": 2,
              "maxItems": 2
            }
        },
        "required": ["limit", "first", "next", "total_count", "items"]
      }
    """
    When I send a GET request to "{{value:next_url}}"
    Then the response code should be 200
    And the response should have schema as:
    """
      {
        "properties": {
            "first": {"type": "object"},
            "next": {
              "type": "object",
              "properties": {
                "href": {"type": "string"}
              },
              "required": ["href"]
            },
            "limit": {"type": "integer"},
            "total_count": {
              "type": "integer",
              "minimum": 3
            },
            "items": {
              "type": "array",
              "minItems": 1
            }
        },
        "required": ["limit", "first", "total_count", "items"]
      }
    """
    When I send a GET request to "/api/v1/evaluations/jobs?owner=test-user-1"
    Then the response code should be 200
    And the response should have schema as:
    """
      {
        "properties": {
          "items": {
            "type": "array",
            "minItems": 1,
            "maxItems": 1
          }
        },
        "required": ["items"]
      }
    """
    And the response should contain the value "test-user-1" at path "$.items[0].resource.owner"
    And the response should contain the value "test-tenant" at path "$.items[0].resource.tenant"
    When I send a GET request to "/api/v1/evaluations/jobs?owner=test-user-2"
    Then the response code should be 200
    And the response should have schema as:
    """
      {
        "properties": {
          "items": {
            "type": "array",
            "minItems": 1,
            "maxItems": 1
          }
        },
        "required": ["items"]
      }
    """
    And the response should contain the value "test-user-2" at path "$.items[0].resource.owner"
    And the response should contain the value "test-tenant" at path "$.items[0].resource.tenant"
    When I send a GET request to "/api/v1/evaluations/jobs?owner=test-user-3"
    Then the response code should be 200
    And the response should have schema as:
    """
      {
        "properties": {
          "items": {
            "type": "array",
            "minItems": 1,
            "maxItems": 1
          }
        },
        "required": ["items"]
      }
    """
    And the response should contain the value "test-user-3" at path "$.items[0].resource.owner"
    And the response should contain the value "test-tenant" at path "$.items[0].resource.tenant"
    When I send a GET request to "/api/v1/evaluations/jobs?owner=test-user-not-3"
    Then the response code should be 200
    And the response should have schema as:
    """
      {
        "properties": {
          "total_count": {
            "type": "number",
            "minimum": 0,
            "maximum": 0
          }
        },
        "required": ["total_count"]
      }
    """

  Scenario: Update evaluation job status with running status
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job.json"
    Then the response code should be 202
    When I send a POST request to "/api/v1/evaluations/jobs/{id}/events" with body "file:/evaluation_job_status_event_running.json"
    Then the response code should be 204
    When I send a GET request to "/api/v1/evaluations/jobs/{id}"
    Then the response code should be 200
    And the response should contain the value "running" at path "$.status.state"
    When I send a POST request to "/api/v1/evaluations/jobs/{id}/events" with body "file:/evaluation_job_status_event_completed.json"
    Then the response code should be 204
    When I send a GET request to "/api/v1/evaluations/jobs/{id}"
    Then the response code should be 200
    And the response should contain the value "completed" at path "$.status.state"
    When I send a DELETE request to "/api/v1/evaluations/jobs/{id}"
    Then the response code should be 409
    And the response should contain the value "can not be cancelled because" at path "$.message"
    When I send a GET request to "/api/v1/evaluations/jobs/{id}"
    Then the response code should be 200
    And the response should contain the value "evaluation_job_updated" at path "$.status.message.message_code"
    And the response should contain the value "completed" at path "$.status.benchmarks[0].status"
    And the response should contain the value "arc_easy" at path "$.status.benchmarks[0].id"
    And the response should contain the value "arc_easy" at path "$.results.benchmarks[0].id"
    And the response should contain the value "lm_evaluation_harness" at path "$.results.benchmarks[0].provider_id"
    And the response should contain the value "5" at path "$.benchmarks[0].parameters.limit"
    And the response should contain the value "google/flan-t5-small" at path "$.benchmarks[0].parameters.tokenizer"
    When I send a DELETE request to "/api/v1/evaluations/jobs/{id}?hard_delete=true"
    Then the response code should be 204

  Scenario: Pass criteria - job and aggregate results after benchmark events
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job_for_pass_criteria_test.json"
    Then the response code should be 202
    When I send a POST request to "/api/v1/evaluations/jobs/{id}/events" with body "file:/evaluation_job_status_event_for_pass_criteria_test_b1.json"
    Then the response code should be 204
    When I send a POST request to "/api/v1/evaluations/jobs/{id}/events" with body "file:/evaluation_job_status_event_for_pass_criteria_test_b2.json"
    Then the response code should be 204
    When I send a GET request to "/api/v1/evaluations/jobs/{id}"
    Then the response code should be 200
    And the response should contain the value "arc_easy" at path "$.results.benchmarks[0].id"
    And the response should contain the value "0.95" at path "$.results.benchmarks[0].test.primary_score"
    And the response should contain the value "true" at path "$.results.benchmarks[0].test.pass"
    And the response should contain the value "AraDiCE_boolq_lev" at path "$.results.benchmarks[1].id"
    And the response should contain the value "0.1" at path "$.results.benchmarks[1].test.primary_score"
    And the response should contain the value "true" at path "$.results.benchmarks[1].test.pass"
    And the response should contain the value "0.92|0.93|0.94" at path "$.results.test.score"
    And the response should contain the value "true" at path "$.results.test.pass"
    When I send a DELETE request to "/api/v1/evaluations/jobs/{id}?hard_delete=true"
    Then the response code should be 204

  Scenario: Pass criteria from provider - test results from provider benchmarks
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/providers" with body "file:/provider_pass_criteria_test.json"
    Then the response code should be 201
    And the "resource.id" field in the response should be saved as "value:provider_id"
    When I send a POST request to "/api/v1/evaluations/collections" with body "file:/collection_pass_criteria_from_provider_test.json"
    Then the response code should be 202
    And the "resource.id" field in the response should be saved as "value:collection_id"
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job_pass_criteria_from_provider_test.json"
    Then the response code should be 202
    And the response should contain the value "{{value:collection_id}}" at path "$.collection.id"
    And the response should contain the value "pending" at path "$.status.state"
    When I send a POST request to "/api/v1/evaluations/jobs/{id}/events" with body "file:/evaluation_job_status_event_pass_criteria_provider_b1.json"
    Then the response code should be 204
    When I send a GET request to "/api/v1/evaluations/jobs/{id}"
    Then the response code should be 200
    And the response should contain the value "pc_b1" at path "$.status.benchmarks[?(@.id == &quot;pc_b1&quot;)].id"
    And the response should contain the value "running" at path "$.status.state"
    And the response should contain the value "completed" at path "$.status.benchmarks[?(@.id == &quot;pc_b1&quot;)].status"
    And the response should contain the value "0.9" at path "$.results.benchmarks[?(@.id == &quot;pc_b1&quot;)].metrics.accuracy"
    And the response should contain the value "2026-01-12T10:45:32Z" at path "$.status.benchmarks[?(@.id == &quot;pc_b1&quot;)].started_at"
    And the response should contain the value "2026-01-12T10:47:12Z" at path "$.status.benchmarks[?(@.id == &quot;pc_b1&quot;)].completed_at"
    When I send a POST request to "/api/v1/evaluations/jobs/{id}/events" with body "file:/evaluation_job_status_event_pass_criteria_provider_b2.json"
    Then the response code should be 204
    When I send a GET request to "/api/v1/evaluations/jobs/{id}"
    Then the response code should be 200
    And the response should contain the value "pc_b1" at path "$.results.benchmarks[?(@.id == &quot;pc_b1&quot;)].id"
    And the response should contain the value "0.9" at path "$.results.benchmarks[?(@.id == &quot;pc_b1&quot;)].test.primary_score"
    And the response should contain the value "true" at path "$.results.benchmarks[?(@.id == &quot;pc_b1&quot;)].test.pass"
    And the response should contain the value "0.5" at path "$.results.benchmarks[?(@.id == &quot;pc_b1&quot;)].test.threshold"
    And the response should contain the value "pc_b2" at path "$.results.benchmarks[?(@.id == &quot;pc_b2&quot;)].id"
    And the response should contain the value "0.8" at path "$.results.benchmarks[?(@.id == &quot;pc_b2&quot;)].test.primary_score"
    And the response should contain the value "true" at path "$.results.benchmarks[?(@.id == &quot;pc_b2&quot;)].test.pass"
    And the response should contain the value "0.6" at path "$.results.benchmarks[?(@.id == &quot;pc_b2&quot;)].test.threshold"
    And the response should contain the value "0.84|0.85|0.86" at path "$.results.test.score"
    And the response should contain the value "true" at path "$.results.test.pass"

    And the response should contain the value "completed" at path "$.status.state"
    When I send a DELETE request to "/api/v1/evaluations/jobs/{id}?hard_delete=true"
    Then the response code should be 204
    When I send a DELETE request to "/api/v1/evaluations/collections/{{value:collection_id}}"
    Then the response code should be 204
    When I send a DELETE request to "/api/v1/evaluations/providers/{{value:provider_id}}"
    Then the response code should be 204

  Scenario: Cancel running evaluation job (soft delete)
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job.json"
    Then the response code should be 202
    When I send a POST request to "/api/v1/evaluations/jobs/{id}/events" with body "file:/evaluation_job_status_event_running.json"
    Then the response code should be 204
    When I send a DELETE request to "/api/v1/evaluations/jobs/{id}"
    Then the response code should be 204
    When I send a GET request to "/api/v1/evaluations/jobs/{id}"
    Then the response code should be 200
    And the response should contain the value "cancelled" at path "$.status.state"
    And the response should contain the value "cancelled" at path "$.status.benchmarks[0].status"
    And the response should contain the value "Evaluation job cancelled" at path "$.status.benchmarks[0].error_message.message"
    And the response should contain the value "evaluation_job_cancelled" at path "$.status.benchmarks[0].error_message.message_code"

  Scenario: Cancel evaluation job with invalid hard_delete query
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job.json"
    Then the response code should be 202
    When I send a DELETE request to "/api/v1/evaluations/jobs/{id}?hard_delete=foo"
    Then the response code should be 400
    And the response should contain the value "query_parameter_invalid" at path "$.message_code"

  Scenario: Update evaluation job status with invalid payload
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job.json"
    Then the response code should be 202
    When I send a POST request to "/api/v1/evaluations/jobs/{id}/events" with body "file:/evaluation_job_status_event_invalid.json"
    Then the response code should be 400
    And the response should contain the value "request_validation_failed" at path "$.message_code"

  Scenario: Update evaluation job status missing provider_id
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job.json"
    Then the response code should be 202
    When I send a POST request to "/api/v1/evaluations/jobs/{id}/events" with body "file:/evaluation_job_status_event_missing_provider.json"
    Then the response code should be 400
    And the response should contain the value "request_validation_failed" at path "$.message_code"

  Scenario: Update evaluation job status for unknown id
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs/unknown-id/events" with body "file:/evaluation_job_status_event_running.json"

  Scenario: List evaluation jobs filtered by status
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job.json"
    Then the response code should be 202
    When I send a POST request to "/api/v1/evaluations/jobs/{id}/events" with body "file:/evaluation_job_status_event_running.json"
    Then the response code should be 204
    When I send a GET request to "/api/v1/evaluations/jobs?status=running&limit=10"
    Then the response code should be 200
    And the response should contain the value "running" at path "$.items[0].status.state"
    And the response should contain the value "1" at path "$.total_count"
    And the response should have schema as:
    """
      {
        "properties": {
            "first": {"type": "object"},
            "next": {
              "type": "object",
              "properties": {
                "href": {"type": "string"}
              },
              "required": ["href"]
            },
            "limit": {"type": "integer"},
            "total_count": {
              "type": "integer",
              "minimum": 1
            },
            "items": {
              "type": "array",
              "minItems": 1,
              "maxItems": 1
            }
        },
        "required": ["limit", "first", "total_count", "items"]
      }
    """
    When I send a POST request to "/api/v1/evaluations/jobs/{id}/events" with body "file:/evaluation_job_status_event_cancelled.json"
    Then the response code should be 204
    When I send a GET request to "/api/v1/evaluations/jobs/{id}"
    Then the response code should be 200
    And the response should contain the value "cancelled" at path "$.status.state"
    When I send a DELETE request to "/api/v1/evaluations/jobs/{id}?hard_delete=true"
    Then the response code should be 204

  Scenario: Partially failed job - one benchmark completed and one failed
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job_for_pass_criteria_test.json"
    Then the response code should be 202
    When I send a POST request to "/api/v1/evaluations/jobs/{id}/events" with body "file:/evaluation_job_status_event_for_pass_criteria_test_b1.json"
    Then the response code should be 204
    When I send a POST request to "/api/v1/evaluations/jobs/{id}/events" with body "file:/evaluation_job_status_event_for_pass_criteria_test_b2_failed.json"
    Then the response code should be 204
    When I send a GET request to "/api/v1/evaluations/jobs/{id}"
    Then the response code should be 200
    And the response should contain the value "partially_failed" at path "$.status.state"
    And the response should contain the value "arc_easy" at path "$.status.benchmarks[0].id"
    And the response should contain the value "completed" at path "$.status.benchmarks[0].status"
    And the response should contain the value "AraDiCE_boolq_lev" at path "$.status.benchmarks[1].id"
    And the response should contain the value "failed" at path "$.status.benchmarks[1].status"
    When I send a DELETE request to "/api/v1/evaluations/jobs/{id}?hard_delete=true"
    Then the response code should be 204

  Scenario: List evaluation jobs returns empty when filter matches no jobs
    Given the service is running
    When I send a GET request to "/api/v1/evaluations/jobs?owner=nonexistent-user-empty-list&limit=10"
    Then the response code should be 200
    And the response should contain the value "0" at path "$.total_count"

  Scenario: List evaluation jobs with all search filters
    Given the service is running
    And there are no evaluation jobs
    When I send a GET request to "/api/v1/evaluations/jobs"
    Then the response code should be 200
    And the response should contain the value "0" at path "$.total_count"
    And I set the header "X-User" to "search-user-a"
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job.json"
    Then the response code should be 202
    When I send a POST request to "/api/v1/evaluations/jobs/{id}/events" with body "file:/evaluation_job_status_event_running.json"
    Then the response code should be 204
    And I set the header "X-User" to "search-user-a"
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job.json"
    Then the response code should be 202
    When I send a POST request to "/api/v1/evaluations/jobs/{id}/events" with body "file:/evaluation_job_status_event_completed.json"
    Then the response code should be 204
    And I set the header "X-User" to "search-user-b"
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job.json"
    Then the response code should be 202
    And I set the header "X-User" to "search-user-b"
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job.json"
    Then the response code should be 202
    When I send a POST request to "/api/v1/evaluations/jobs/{id}/events" with body "file:/evaluation_job_status_event_completed.json"
    Then the response code should be 204
    When I send a GET request to "/api/v1/evaluations/jobs"
    Then the response code should be 200
    And the response should contain the value "4" at path "$.total_count"
    When I send a GET request to "/api/v1/evaluations/jobs?status=running&owner=search-user-a&limit=10"
    Then the response code should be 200
    And the response should contain the value "running" at path "$.items[0].status.state"
    And the response should contain the value "search-user-a" at path "$.items[0].resource.owner"
    And the response should contain the value "1" at path "$.total_count"
    When I send a GET request to "/api/v1/evaluations/jobs?status=completed&owner=search-user-a&limit=10"
    Then the response code should be 200
    And the response should contain the value "completed" at path "$.items[0].status.state"
    And the response should contain the value "search-user-a" at path "$.items[0].resource.owner"
    And the response should contain the value "1" at path "$.total_count"
    When I send a GET request to "/api/v1/evaluations/jobs?status=pending&owner=search-user-b&limit=10"
    Then the response code should be 200
    And the response should contain the value "pending" at path "$.items[0].status.state"
    And the response should contain the value "search-user-b" at path "$.items[0].resource.owner"
    And the response should contain the value "1" at path "$.total_count"
    When I send a GET request to "/api/v1/evaluations/jobs?owner=search-user-a&limit=10"
    Then the response code should be 200
    And the response should contain the value "search-user-a" at path "$.items[0].resource.owner"
    And the response should contain the value "2" at path "$.total_count"
    When I send a GET request to "/api/v1/evaluations/jobs?owner=search-user-b&limit=10"
    Then the response code should be 200
    And the response should contain the value "search-user-b" at path "$.items[0].resource.owner"
    And the response should contain the value "2" at path "$.total_count"
    When I send a GET request to "/api/v1/evaluations/jobs?limit=10"
    Then the response code should be 200
    And the response should contain the value "test-tenant" at path "$.items[0].resource.tenant"
    And the response should contain the value "4" at path "$.total_count"
    When I send a GET request to "/api/v1/evaluations/jobs?owner=search-user-a&limit=10"
    Then the response code should be 200
    And the response should contain the value "search-user-a" at path "$.items[0].resource.owner"
    And the response should contain the value "test-tenant" at path "$.items[0].resource.tenant"
    And the response should contain the value "2" at path "$.total_count"
    When I send a GET request to "/api/v1/evaluations/jobs?owner=search-user-b&status=completed&limit=10"
    Then the response code should be 200
    And the response should contain the value "search-user-b" at path "$.items[0].resource.owner"
    And the response should contain the value "test-tenant" at path "$.items[0].resource.tenant"
    And the response should contain the value "completed" at path "$.items[0].status.state"
    And the response should contain the value "1" at path "$.total_count"
    When I send a GET request to "/api/v1/evaluations/jobs?name=my-test-experiment&owner=search-user-a&limit=10"
    Then the response code should be 200
    And the response should contain the value "0" at path "$.total_count"
    When I set transaction-id to "search-user-and-tags"
    When I send a GET request to "/api/v1/evaluations/jobs?tags=environment&owner=search-user-a&limit=10"
    Then the response code should be 200
    And the response should contain the value "2" at path "$.total_count"
    And the response should contain the value "search-user-a" at path "$.items[0].resource.owner"
    And the response should contain the value "environment" at path "$.items[0].tags[0]"
    When I send a GET request to "/api/v1/evaluations/jobs?tags=doesnotexist&limit=10"
    Then the response code should be 200
    And the response should contain the value "0" at path "$.total_count"

  Scenario: Evaluation endpoints reject unsupported methods
    Given the service is running
    When I send a PUT request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job.json"
    Then the response code should be 405
    When I send a POST request to "/api/v1/evaluations/jobs/unknown-id" with body "file:/evaluation_job.json"
    Then the response code should be 405
    When I send a GET request to "/api/v1/evaluations/jobs/unknown-id/events"
    Then the response code should be 405
