Feature: MCP Server Initialization
  As an MCP client
  I want to connect to the EvalHub MCP server
  So that I can discover its capabilities

  Scenario: Server reports correct metadata
    Given an MCP server is running
    When I connect to the MCP server
    Then the server name should be "evalhub-mcp"
    And the server version should not be empty

  Scenario: Server advertises all capabilities
    Given an MCP server is running
    When I connect to the MCP server
    Then the server should advertise tools capability
    And the server should advertise resources capability
    And the server should advertise prompts capability
    And the server should advertise logging capability

  Scenario: Server lists tools when backend is connected
    Given an MCP server is running with a backend
    When I list tools
    Then the tools list should contain "submit_evaluation"
    And the tools list should contain "cancel_job"
    And the tools list should contain "get_job_status"
    And the tools list should contain "discover_providers"

  Scenario: Server lists no tools without backend
    Given an MCP server is running
    When I list tools
    Then the tools list should have length 0

  Scenario: Server lists resources when backend is connected
    Given an MCP server is running with a backend
    When I list resources
    Then the resources list should contain URI "evalhub://providers"
    And the resources list should contain URI "evalhub://benchmarks"
    And the resources list should contain URI "evalhub://collections"
    And the resources list should contain URI "evalhub://jobs"
    And the resources list should contain URI "evalhub://server/version"

  Scenario: Server lists only version resource without backend
    Given an MCP server is running
    When I list resources
    Then the resources list should have length 1
    And the resources list should contain URI "evalhub://server/version"

  Scenario: Server lists prompts when backend is connected
    Given an MCP server is running with a backend
    When I list prompts
    Then the prompts list should contain "edd_workflow"
    And the prompts list should contain "evaluate_model"
    And the prompts list should contain "compare_runs"

  Scenario: Server lists no prompts without backend
    Given an MCP server is running
    When I list prompts
    Then the prompts list should have length 0
