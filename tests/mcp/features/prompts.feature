Feature: MCP Prompts
  As an MCP client
  I want to get EvalHub MCP prompts
  So that I can guide evaluation workflows

  Background:
    Given an MCP server is running with a backend

  Scenario: Get EDD workflow prompt for RAG application
    When I get the prompt "edd_workflow" with arguments:
      | key              | value |
      | application_type | rag   |
    Then the prompt should succeed
    And the prompt result should have at least 1 message

  Scenario: Get EDD workflow prompt for agent application
    When I get the prompt "edd_workflow" with arguments:
      | key              | value |
      | application_type | agent |
    Then the prompt should succeed
    And the prompt result should have at least 1 message

  Scenario: Get EDD workflow prompt fails without application type
    When I get the prompt "edd_workflow" with arguments:
      | key              | value |
      | application_type |       |
    Then the prompt should fail

  Scenario: Get EDD workflow prompt fails with invalid application type
    When I get the prompt "edd_workflow" with arguments:
      | key              | value   |
      | application_type | invalid |
    Then the prompt should fail

  Scenario: Get evaluate model prompt without model URL
    When I get the prompt "evaluate_model" with arguments:
      | key       | value |
      | model_url |       |
    Then the prompt should succeed
    And the prompt result should have at least 1 message

  Scenario: Get evaluate model prompt with model URL
    When I get the prompt "evaluate_model" with arguments:
      | key       | value              |
      | model_url | http://model:8080  |
    Then the prompt should succeed
    And the prompt result should have at least 1 message

  Scenario: Get compare runs prompt with job IDs
    When I get the prompt "compare_runs" with arguments:
      | key     | value       |
      | job_ids | job-1,job-2 |
    Then the prompt should succeed
    And the prompt result should have at least 1 message

  Scenario: Get compare runs prompt fails with single job ID
    When I get the prompt "compare_runs" with arguments:
      | key     | value |
      | job_ids | job-1 |
    Then the prompt should fail
