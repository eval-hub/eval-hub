Feature: MCP Tools
  As an MCP client
  I want to call EvalHub MCP tools
  So that I can manage evaluations programmatically

  Background:
    Given an MCP server is running with a backend

  Scenario: Discover providers returns results
    When I call the tool "discover_providers" with arguments:
      """
      {}
      """
    Then the tool call should succeed
    And the tool result should contain "Found 2 providers"

  Scenario: Discover providers with target type filter
    When I call the tool "discover_providers" with arguments:
      """
      {"target_type": "model"}
      """
    Then the tool call should succeed

  Scenario: Submit evaluation with benchmarks
    When I call the tool "submit_evaluation" with arguments:
      """
      {
        "name": "test-eval",
        "model": {"url": "http://model:8080", "name": "test-model"},
        "benchmarks": [{"id": "hellaswag", "provider_id": "lighteval"}]
      }
      """
    Then the tool call should succeed
    And the tool result should contain "Evaluation job created"

  Scenario: Submit evaluation with collection
    When I call the tool "submit_evaluation" with arguments:
      """
      {
        "name": "test-eval-collection",
        "model": {"url": "http://model:8080", "name": "test-model"},
        "collection": {"id": "safety-suite"}
      }
      """
    Then the tool call should succeed
    And the tool result should contain "Evaluation job created"

  Scenario: Submit evaluation fails without benchmarks or collection
    When I call the tool "submit_evaluation" with arguments:
      """
      {
        "name": "test-eval",
        "model": {"url": "http://model:8080", "name": "test-model"}
      }
      """
    Then the tool call should return an error
    And the tool result should contain "provide at least one of"

  Scenario: Submit evaluation fails with both benchmarks and collection
    When I call the tool "submit_evaluation" with arguments:
      """
      {
        "name": "test-eval",
        "model": {"url": "http://model:8080", "name": "test-model"},
        "benchmarks": [{"id": "hellaswag", "provider_id": "lighteval"}],
        "collection": {"id": "safety-suite"}
      }
      """
    Then the tool call should return an error
    And the tool result should contain "not both"

  Scenario: Get job status for existing job
    When I call the tool "get_job_status" with arguments:
      """
      {"job_id": "job-1"}
      """
    Then the tool call should succeed
    And the tool result should contain "job-1"
    And the tool result should contain "running"

  Scenario: Get job status for non-existent job
    When I call the tool "get_job_status" with arguments:
      """
      {"job_id": "non-existent"}
      """
    Then the tool call should return an error
    And the tool result should contain "failed to get job status"

  Scenario: Get job status fails without job ID
    When I call the tool "get_job_status" with arguments:
      """
      {"job_id": ""}
      """
    Then the tool call should return an error
    And the tool result should contain "job_id"

  Scenario: Cancel job succeeds
    When I call the tool "cancel_job" with arguments:
      """
      {"job_id": "job-1"}
      """
    Then the tool call should succeed
    And the tool result should contain "cancelled successfully"

  Scenario: Cancel job fails without job ID
    When I call the tool "cancel_job" with arguments:
      """
      {"job_id": ""}
      """
    Then the tool call should return an error
    And the tool result should contain "job_id"
