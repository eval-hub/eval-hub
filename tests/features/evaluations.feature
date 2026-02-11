Feature: Evaluations Endpoint
  As a data scientist
  I want to create evaluation jobs
  So that I evaluate models

  Scenario: Create an evaluation job
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job.json"
    Then the response code should be 202
    When I send a GET request to "/api/v1/evaluations/jobs/{id}"
    Then the response code should be 200
    And the response should contain the value "pending" at path "$.status.state"
    And the response should not contain the value "collection" at path "$."
    When I send a DELETE request to "/api/v1/evaluations/jobs/{id}?hard_delete=true"
    Then the response code should be 204
    When I send a GET request to "/api/v1/evaluations/jobs/{id}"
    Then the response code should be 404
    When I send a DELETE request to "/api/v1/evaluations/jobs/{id}?hard_delete=true"
    Then the response code should be 404

  Scenario: List evaluation jobs
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job.json"
    Then the response code should be 202
    And I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job.json"
    Then the response code should be 202
    And I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job.json"
    Then the response code should be 202
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

  Scenario: Update evaluation job status with running status
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job.json"
    Then the response code should be 202
    When I send a POST request to "/api/v1/evaluations/jobs/{id}/events" with body "file:/evaluation_job_status_event_running.json"
    Then the response code should be 204
    When I send a GET request to "/api/v1/evaluations/jobs/{id}"
    Then the response code should be 200
    And the response should contain the value "running" at path "$.status.state"
    When I send a DELETE request to "/api/v1/evaluations/jobs/{id}"
    Then the response code should be 204
    When I send a GET request to "/api/v1/evaluations/jobs/{id}"
    Then the response code should be 200
    And the response should contain the value "cancelled" at path "$.status.state"
    When I send a POST request to "/api/v1/evaluations/jobs/{id}/events" with body "file:/evaluation_job_status_event_completed.json"
    Then the response code should be 204
    When I send a GET request to "/api/v1/evaluations/jobs/{id}"
    Then the response code should be 200
    And the response should contain the value "completed" at path "$.status.state"
    When I send a DELETE request to "/api/v1/evaluations/jobs/{id}"
    Then the response code should be 409
    And the response should contain the value "can not be cancelled because" at path "$.message"
    When I send a DELETE request to "/api/v1/evaluations/jobs/{id}?hard_delete=true"
    Then the response code should be 204
