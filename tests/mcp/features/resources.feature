Feature: MCP Resources
  As an MCP client
  I want to read EvalHub MCP resources
  So that I can discover evaluation data

  Background:
    Given an MCP server is running with a backend

  Scenario: Read server version resource
    When I read the resource "evalhub://server/version"
    Then the resource read should succeed
    And the resource response should contain "version"
    And the resource response should contain "go_version"
    And the resource response should contain "mcp_library"

  Scenario: Read providers list
    When I read the resource "evalhub://providers"
    Then the resource read should succeed
    And the resource response should be a JSON array with at least 1 item

  Scenario: Read a specific provider
    When I read the resource "evalhub://providers/lighteval"
    Then the resource read should succeed
    And the resource response should contain "lighteval"

  Scenario: Read non-existent provider
    When I read the resource "evalhub://providers/non-existent"
    Then the resource read should fail

  Scenario: Read benchmarks list
    When I read the resource "evalhub://benchmarks"
    Then the resource read should succeed
    And the resource response should be a JSON array with at least 1 item

  Scenario: Read a specific benchmark
    When I read the resource "evalhub://benchmarks/hellaswag"
    Then the resource read should succeed
    And the resource response should contain "hellaswag"

  Scenario: Read benchmarks filtered by label
    When I read the resource "evalhub://benchmarks?label=safety"
    Then the resource read should succeed
    And the resource response should be a JSON array with at least 1 item

  Scenario: Read collections list
    When I read the resource "evalhub://collections"
    Then the resource read should succeed
    And the resource response should be a JSON array with at least 1 item

  Scenario: Read a specific collection
    When I read the resource "evalhub://collections/safety-suite"
    Then the resource read should succeed
    And the resource response should contain "Safety Suite"

  Scenario: Read jobs list
    When I read the resource "evalhub://jobs"
    Then the resource read should succeed
    And the resource response should be a JSON array with at least 1 item

  Scenario: Read a specific job
    When I read the resource "evalhub://jobs/job-1"
    Then the resource read should succeed
    And the resource response should contain "job-1"

  Scenario: Read jobs filtered by status
    When I read the resource "evalhub://jobs?status=completed"
    Then the resource read should succeed
    And the resource response should be a JSON array with at least 1 item
