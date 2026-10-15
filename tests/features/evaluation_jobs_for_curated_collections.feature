@cluster
@evaluations
@benchmark_providers
@curatedcollections

Feature: Collections Endpoint
  As a data scientist
  I want to run all curated collections of benchmarks
  So that I evaluate models on these collections

  Background:
    Given I set the header "X-Tenant" to "{{env:X_TENANT|test-tenant}}"
    And I set the header "X-User" to "{{env:X_USER|test-user}}"


  Scenario: Verify curated collection run and results - document-understanding-v1
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job_document-understanding-v1.json"
    Then the response code should be 202
    And the response should contain the value "document-understanding-v1" at path "$.collection.id"
    And I wait for the evaluation job status to be "completed"
    When I send a GET request to "/api/v1/evaluations/jobs/{id}"
    Then the response code should be 200
    And the response should contain the value "document-understanding-v1" at path "$.collection.id"
    And the array at path "$.collection.benchmarks" in the response should have length 7
    And the response should contain the value "common-words-extraction" at path "$.collection.benchmarks[*].id"
    And the response should contain the value "frequency-words-extraction" at path "$.collection.benchmarks[*].id"
    And the response should contain the value "qa-squad" at path "$.collection.benchmarks[*].id"
    And the response should contain the value "qa-hotpotqa" at path "$.collection.benchmarks[*].id"
    And the response should contain the value "niah-single-essay" at path "$.collection.benchmarks[*].id"
    And the response should contain the value "variable-tracking" at path "$.collection.benchmarks[*].id"
    And the response should contain the value "longbenchv2" at path "$.collection.benchmarks[*].id"
    And the response should contain the value "completed" at path "$.status.state"
    And the response should contain the value "Evaluation job is completed" at path "$.status.message.message"
    And the array at path "$.results.benchmarks" in the response should have length 7
    And all benchmarks in the response should have status "completed"
    And all benchmarks in the response should have metrics
    And the response should contain the value "common-words-extraction" at path "$.results.benchmarks[*].id"
    And the response should contain the value "frequency-words-extraction" at path "$.results.benchmarks[*].id"
    And the response should contain the value "qa-squad" at path "$.results.benchmarks[*].id"
    And the response should contain the value "qa-hotpotqa" at path "$.results.benchmarks[*].id"
    And the response should contain the value "niah-single-essay" at path "$.results.benchmarks[*].id"
    And the response should contain the value "variable-tracking" at path "$.results.benchmarks[*].id"
    And the response should contain the value "longbenchv2" at path "$.results.benchmarks[*].id"
    And the response should contain "results"

  # currently not present on any cluster
  Scenario: Verify curated collection run and results - fairness-bias-v1
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job_fairness-bias-v1.json"
    Then the response code should be 202
    And the response should contain the value "fairness-bias-v1" at path "$.collection.id"
    And I wait for the evaluation job status to be "completed"
    When I send a GET request to "/api/v1/evaluations/jobs/{id}"
    Then the response code should be 200
    And the response should contain the value "fairness-bias-v1" at path "$.collection.id"
    And the response should contain the value "completed" at path "$.status.state"
    And the response should contain the value "Evaluation job is completed" at path "$.status.message.message"
    And the array at path "$.results.benchmarks" in the response should have length 3
    And all benchmarks in the response should have status "completed"
    And all benchmarks in the response should have metrics
    And the response should contain the value "winogender" at path "$.results.benchmarks[*].id"
    And the response should contain the value "crows_pairs_english" at path "$.results.benchmarks[*].id"
    And the response should contain the value "bbq" at path "$.results.benchmarks[*].id"
    And the response should contain "results"

  Scenario: Verify curated collection run and results - instruction-output-v1
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job_instruction-output-v1.json"
    Then the response code should be 202
    And the response should contain the value "instruction-output-v1" at path "$.collection.id"
    And I wait for the evaluation job status to be "completed"
    When I send a GET request to "/api/v1/evaluations/jobs/{id}"
    Then the response code should be 200
    And the response should contain the value "instruction-output-v1" at path "$.collection.id"
    And the response should contain the value "completed" at path "$.status.state"
    And the response should contain the value "Evaluation job is completed" at path "$.status.message.message"
    And the array at path "$.results.benchmarks" in the response should have length 3
    And all benchmarks in the response should have status "completed"
    And all benchmarks in the response should have metrics
    And the response should contain the value "ifbench" at path "$.results.benchmarks[*].id"
    And the response should contain the value "jsonschema_bench_easy" at path "$.results.benchmarks[*].id"
    And the response should contain the value "followbench" at path "$.results.benchmarks[*].id"
    And the response should contain "results"

  Scenario: Verify curated collection run and results - knowledge-reasoning-v1
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job_knowledge-reasoning-v1.json"
    Then the response code should be 202
    And the response should contain the value "knowledge-reasoning-v1" at path "$.collection.id"
    And I wait for the evaluation job status to be "completed"
    When I send a GET request to "/api/v1/evaluations/jobs/{id}"
    Then the response code should be 200
    And the response should contain the value "knowledge-reasoning-v1" at path "$.collection.id"
    And the response should contain the value "completed" at path "$.status.state"
    And the response should contain the value "Evaluation job is completed" at path "$.status.message.message"
    And the array at path "$.results.benchmarks" in the response should have length 3
    And all benchmarks in the response should have status "completed"
    And all benchmarks in the response should have metrics
    And the response should contain the value "gpqa:diamond" at path "$.results.benchmarks[*].id"
    And the response should contain the value "inspect/hle" at path "$.results.benchmarks[*].id"
    And the response should contain the value "math_500" at path "$.results.benchmarks[*].id"
    And the response should contain "results"

  Scenario: Verify curated collection run and results - multimodal-v1
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job_multimodal-v1.json"
    Then the response code should be 202
    And the response should contain the value "multimodal-v1" at path "$.collection.id"
    And I wait for the evaluation job status to be "completed"
    When I send a GET request to "/api/v1/evaluations/jobs/{id}"
    Then the response code should be 200
    And the response should contain the value "multimodal-v1" at path "$.collection.id"
    And the response should contain the value "completed" at path "$.status.state"
    And the response should contain the value "Evaluation job is completed" at path "$.status.message.message"
    And the array at path "$.results.benchmarks" in the response should have length 1
    And all benchmarks in the response should have status "completed"
    And all benchmarks in the response should have metrics
    And the response should contain the value "inspect/docvqa" at path "$.results.benchmarks[*].id"
    And the response should contain "results"

  # currently not present on any cluster
  Scenario: Verify curated collection run and results - safety-ethical-principles-v1
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job_safety-ethical-principles-v1.json"
    Then the response code should be 202
    And the response should contain the value "safety-ethical-principles-v1" at path "$.collection.id"
    And I wait for the evaluation job status to be "completed"
    When I send a GET request to "/api/v1/evaluations/jobs/{id}"
    Then the response code should be 200
    And the response should contain the value "safety-ethical-principles-v1" at path "$.collection.id"
    And the response should contain the value "completed" at path "$.status.state"
    And the response should contain the value "Evaluation job is completed" at path "$.status.message.message"
    And the array at path "$.results.benchmarks" in the response should have length 3
    And all benchmarks in the response should have status "completed"
    And all benchmarks in the response should have metrics
    And the response should contain the value "toxigen" at path "$.results.benchmarks[*].id"
    And the response should contain the value "ethics_cm" at path "$.results.benchmarks[*].id"
    And the response should contain the value "bigbench_hhh_alignment_multiple_choice" at path "$.results.benchmarks[*].id"
    And the response should contain "results"


  Scenario: Verify curated collection run and results - software-v1
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job_software-v1.json"
    Then the response code should be 202
    And the response should contain the value "software-v1" at path "$.collection.id"
    And I wait for the evaluation job status to be "completed"
    When I send a GET request to "/api/v1/evaluations/jobs/{id}"
    Then the response code should be 200
    And the response should contain the value "software-v1" at path "$.collection.id"
    And the response should contain the value "completed" at path "$.status.state"
    And the response should contain the value "Evaluation job is completed" at path "$.status.message.message"
    And the array at path "$.results.benchmarks" in the response should have length 3
    And all benchmarks in the response should have status "completed"
    And all benchmarks in the response should have metrics
    And the response should contain the value "lcb:codegeneration_v6" at path "$.results.benchmarks[*].id"
    And the response should contain the value "inspect/bigcodebench" at path "$.results.benchmarks[*].id"
    And the response should contain the value "cruxeval_output" at path "$.results.benchmarks[*].id"
    And the response should contain "results"

  Scenario: Verify curated collection run and results - tool-use-v1
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job_tool-use-v1.json"
    Then the response code should be 202
    And the response should contain the value "tool-use-v1" at path "$.collection.id"
    And I wait for the evaluation job status to be "completed"
    When I send a GET request to "/api/v1/evaluations/jobs/{id}"
    Then the response code should be 200
    And the response should contain the value "tool-use-v1" at path "$.collection.id"
    And the response should contain the value "completed" at path "$.status.state"
    And the response should contain the value "Evaluation job is completed" at path "$.status.message.message"
    And the array at path "$.results.benchmarks" in the response should have length 2
    And all benchmarks in the response should have status "completed"
    And all benchmarks in the response should have metrics
    And the benchmark "inspect/bfcl" in the response should have metric "bfcl_scorer/accuracy"
    And the response should contain the value "inspect/bfcl" at path "$.results.benchmarks[*].id"
    And the response should contain "results"

  Scenario: Verify curated collection run and results - trustworthiness-v1
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job_trustworthiness-v1.json"
    Then the response code should be 202
    And the response should contain the value "trustworthiness-v1" at path "$.collection.id"
    And I wait for the evaluation job status to be "completed"
    When I send a GET request to "/api/v1/evaluations/jobs/{id}"
    Then the response code should be 200
    And the response should contain the value "trustworthiness-v1" at path "$.collection.id"
    And the response should contain the value "completed" at path "$.status.state"
    And the response should contain the value "Evaluation job is completed" at path "$.status.message.message"
    And the array at path "$.results.benchmarks" in the response should have length 2
    And all benchmarks in the response should have status "completed"
    And all benchmarks in the response should have metrics
    And the response should contain the value "inspect/cyberseceval-2-pi" at path "$.results.benchmarks[*].id"
    And the response should contain the value "truthfulqa_mc1" at path "$.results.benchmarks[*].id"
    And the response should contain "results"
