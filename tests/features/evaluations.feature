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
    # TODO And the response should contain the value "pending" in the "status" field
    When I send a DELETE request to "/api/v1/evaluations/jobs/{id}?hard_delete=true"
    Then the response code should be 204
    When I send a GET request to "/api/v1/evaluations/jobs/{id}"
    Then the response code should be 404
