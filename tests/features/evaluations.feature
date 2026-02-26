@evaluations
Feature: Evaluations Endpoint
  As a data scientist
  I want to create evaluation jobs
  So that I evaluate models

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
    And I wait for the evaluation job status to be "completed"
    When I send a DELETE request to "/api/v1/evaluations/jobs/{id}?hard_delete=true"
    Then the response code should be 204
    When I send a GET request to "/api/v1/evaluations/jobs/{id}"
    Then the response code should be 404
    And the response should contain the value "resource_not_found" at path "$.message_code"
    When I send a DELETE request to "/api/v1/evaluations/jobs/{id}?hard_delete=true"
    Then the response code should be 404

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

  Scenario: List evaluation jobs
    Given the service is running
    And I set the header "X-User" to "test-user-1"
    And I set the header "X-Tenant" to "test-tenant-1"
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job.json"
    Then the response code should be 202
    And the response should contain the value "test-user-1" at path "$.resource.owner"
    And the response should contain the value "test-tenant-1" at path "$.resource.tenant"
    And I set the header "X-User" to "test-user-2"
    And I set the header "X-Tenant" to "test-tenant-2"
    And I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job.json"
    Then the response code should be 202
    And the response should contain the value "test-user-2" at path "$.resource.owner"
    And the response should contain the value "test-tenant-2" at path "$.resource.tenant"
    And I set the header "X-User" to "test-user-3"
    And I set the header "X-Tenant" to "test-tenant-3"
    And I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job.json"
    Then the response code should be 202
    And the response should contain the value "test-user-3" at path "$.resource.owner"
    And the response should contain the value "test-tenant-3" at path "$.resource.tenant"
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
    When I send a GET request to "/api/v1/evaluations/jobs?owner=test-user-1&tenant=test-tenant-1"
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
    When I send a GET request to "/api/v1/evaluations/jobs?owner=test-user-2&tenant=test-tenant-2"
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
    When I send a GET request to "/api/v1/evaluations/jobs?owner=test-user-3&tenant=test-tenant-3"
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
    Then the response code should be 404

  Scenario: List evaluation jobs filtered by status
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job.json"
    Then the response code should be 202
    When I send a POST request to "/api/v1/evaluations/jobs/{id}/events" with body "file:/evaluation_job_status_event_running.json"
    Then the response code should be 204
    When I send a GET request to "/api/v1/evaluations/jobs?status=running&limit=1"
    Then the response code should be 200
    And the response should contain the value "running" at path "$.items[0].status.state"
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
    When I send a DELETE request to "/api/v1/evaluations/jobs/{id}?hard_delete=true"
    Then the response code should be 204

  Scenario: Evaluation endpoints reject unsupported methods
    Given the service is running
    When I send a PUT request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job.json"
    Then the response code should be 405
    When I send a POST request to "/api/v1/evaluations/jobs/unknown-id" with body "file:/evaluation_job.json"
    Then the response code should be 405
    When I send a GET request to "/api/v1/evaluations/jobs/unknown-id/events"
    Then the response code should be 405
