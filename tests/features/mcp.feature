@mcp
Feature: MCP Tools
  As a user
  I want to call MCP tools
  So that I can interact with eval-hub via MCP protocol

  Background:
    Given the service is running
    And I set the wait deadline to "{{env:WAIT_DEADLINE|30m}}"
  
  Scenario: Discover all providers via MCP returns unfiltered list
    When I call MCP tool "discover_providers" with arguments "{}"
    Then the MCP tool call should succeed
    And the MCP response should contain "lm_evaluation_harness"
    And the MCP response should contain "lighteval"
    And the MCP response should contain "garak"

  Scenario: Discover providers filtered by target_type model returns only model providers
    When I call MCP tool "discover_providers" with arguments:
      """
      {
        "target_type": "model"
      }
      """
    Then the MCP tool call should succeed
    And the MCP response should contain "lm_evaluation_harness"
    And the MCP response should contain "target_type"
    And the MCP response should contain "model"

  Scenario: Discover providers filtered by evaluates returns only matching providers
    When I call MCP tool "discover_providers" with arguments:
      """
      {
        "evaluates": ["accuracy"]
      }
      """
    Then the MCP tool call should succeed
    And the MCP response should contain "lm_evaluation_harness"
    And the MCP response should contain "evaluates"
    And the MCP response should contain "accuracy"

  Scenario: Discover providers with combined filters target_type and evaluates
    When I call MCP tool "discover_providers" with arguments:
      """
      {
        "target_type": "model",
        "evaluates": ["accuracy"]
      }
      """
    Then the MCP tool call should succeed
    And the MCP response should contain "lm_evaluation_harness"
    And the MCP response should contain "target_type"
    And the MCP response should contain "model"
    And the MCP response should contain "accuracy"

  Scenario: Discover providers filtered by target_type agent returns empty when no matches
    When I call MCP tool "discover_providers" with arguments:
      """
      {
        "target_type": "agent"
      }
      """
    Then the MCP tool call should succeed
    And the MCP response should contain "providers"
    And the MCP response array at path "providers" should have length 0

  Scenario: Discover providers filtered by non-matching evaluates returns empty
    When I call MCP tool "discover_providers" with arguments:
      """
      {
        "evaluates": ["nonexistent_capability_xyz"]
      }
      """
    Then the MCP tool call should succeed
    And the MCP response should contain "providers"
    And the MCP response array at path "providers" should have length 0

  Scenario: Discover providers includes complete agent metadata structure
    When I call MCP tool "discover_providers" with arguments:
      """
      {
        "target_type": "model"
      }
      """
    Then the MCP tool call should succeed
    And the MCP response array at path "providers" should have length at least 1
    And the MCP response at JSONPath "$.providers[?(@.id==\"lm_evaluation_harness\")].target_type" should equal "model"
    And the MCP response at JSONPath "$.providers[?(@.id==\"lm_evaluation_harness\")].summary" should not be empty
    And the MCP response at JSONPath "$.providers[?(@.id==\"lm_evaluation_harness\")].evaluates" should be an array
    And the MCP response at JSONPath "$.providers[?(@.id==\"lm_evaluation_harness\")].evaluates" should have at least 1 items
    And the MCP response at JSONPath "$.providers[?(@.id==\"lm_evaluation_harness\")].hints" should be an array
    And the MCP response at JSONPath "$.providers[?(@.id==\"lm_evaluation_harness\")].hints" should have at least 1 items
    And the MCP response at JSONPath "$.providers[?(@.id==\"lm_evaluation_harness\")].recommended_when" should be an array
    And the MCP response at JSONPath "$.providers[?(@.id==\"lm_evaluation_harness\")].recommended_when" should have at least 1 items
    And the MCP response at JSONPath "$.providers[?(@.id==\"lm_evaluation_harness\")].complements" should be an array
    And the MCP response at JSONPath "$.providers[?(@.id==\"lm_evaluation_harness\")].complements" should have at least 1 items
    And the MCP response at JSONPath "$.providers[?(@.id==\"lm_evaluation_harness\")].result_interpretation" should be an array
    And the MCP response at JSONPath "$.providers[?(@.id==\"lm_evaluation_harness\")].result_interpretation" should have at least 1 items

  Scenario: Discover providers filtered by multiple evaluates capabilities validates AND logic
    When I call MCP tool "discover_providers" with arguments:
      """
      {
        "evaluates": ["accuracy", "reasoning"]
      }
      """
    Then the MCP tool call should succeed
    And the MCP response array at path "providers" should have length at least 1
    # Verify lm_evaluation_harness matches (has both accuracy AND reasoning)
    And the MCP response should contain "lm_evaluation_harness"
    And the MCP response at JSONPath "$.providers[?(@.id==\"lm_evaluation_harness\")].evaluates" should be an array
    And the MCP response at JSONPath "$.providers[?(@.id==\"lm_evaluation_harness\")].evaluates" should have at least 2 items

  Scenario: User provider with agent metadata appears in discover_providers with complete fields
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/providers" with body "file:/user_provider_with_agent_metadata_full.json"
    Then the response code should be 201
    When I call MCP tool "discover_providers" with arguments:
      """
      {
        "target_type": "model"
      }
      """
    Then the MCP tool call should succeed
    And the MCP response array at path "providers" should have length at least 2
    And the MCP response at JSONPath "$.providers[?(@.name==\"my-custom-evaluator\")].title" should equal "Custom Internal Evaluator"
    And the MCP response at JSONPath "$.providers[?(@.name==\"my-custom-evaluator\")].summary" should equal "Custom evaluator for internal domain-specific benchmarks"
    And the MCP response at JSONPath "$.providers[?(@.name==\"my-custom-evaluator\")].target_type" should equal "model"
    And the MCP response at JSONPath "$.providers[?(@.name==\"my-custom-evaluator\")].evaluates" should be an array
    And the MCP response at JSONPath "$.providers[?(@.name==\"my-custom-evaluator\")].evaluates" should have at least 3 items
    And the MCP response at JSONPath "$.providers[?(@.name==\"my-custom-evaluator\")].hints" should be an array
    And the MCP response at JSONPath "$.providers[?(@.name==\"my-custom-evaluator\")].hints" should have at least 2 items
    And the MCP response at JSONPath "$.providers[?(@.name==\"my-custom-evaluator\")].result_interpretation" should be an array
    And the MCP response at JSONPath "$.providers[?(@.name==\"my-custom-evaluator\")].result_interpretation" should have at least 2 items
    And the MCP response at JSONPath "$.providers[?(@.name==\"my-custom-evaluator\")].recommended_when" should be an array
    And the MCP response at JSONPath "$.providers[?(@.name==\"my-custom-evaluator\")].recommended_when" should have at least 2 items
    And the MCP response at JSONPath "$.providers[?(@.name==\"my-custom-evaluator\")].complements" should be an array
    And the MCP response at JSONPath "$.providers[?(@.name==\"my-custom-evaluator\")].complements" should have at least 1 items

  Scenario: Discover providers then submit evaluation job with known provider
    When I call MCP tool "discover_providers" with arguments:
      """
      {
        "target_type": "model",
        "evaluates": ["accuracy"]
      }
      """
    Then the MCP tool call should succeed
    And the MCP response should contain "lm_evaluation_harness"
    When I call MCP tool "submit_evaluation" with arguments:
      """
      {
        "name": "discovered_provider_job",
        "description": "Job using provider discovered via discover_providers",
        "model": {
          "url": "{{env:MODEL_URL|http://test.com}}",
          "name": "{{env:MODEL_NAME|test}}"
        },
        "benchmarks": [
          {
            "id": "arc_easy",
            "provider_id": "lm_evaluation_harness"
          }
        ]
      }
      """
    Then the MCP tool call should succeed
    And the MCP response should contain the value "pending" at path "$.state"

  Scenario: Discover providers filtered by target_type agent returns matching providers
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/providers" with body:
      """
      {
        "name": "test-agent-provider-mcp",
        "title": "Test Agent Provider for MCP",
        "description": "Agent provider for MCP discover test",
        "agent": {
          "evaluates": ["reasoning", "planning"],
          "target_type": "agent",
          "summary": "Evaluates AI agent capabilities"
        },
        "benchmarks": [
          {
            "id": "test_agent_benchmark",
            "name": "Agent Test Benchmark"
          }
        ]
      }
      """
    Then the response code should be 201
    # Now discover providers with target_type agent filter
    When I call MCP tool "discover_providers" with arguments:
      """
      {
        "target_type": "agent"
      }
      """
    Then the MCP tool call should succeed
    And the MCP response should contain "test-agent-provider-mcp"
    And the MCP response at JSONPath "$.providers[?(@.name==\"test-agent-provider-mcp\")].target_type" should equal "agent"

  Scenario: Discover providers filtered by target_type inference_server returns matching providers
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/providers" with body:
      """
      {
        "name": "test-inference-server-provider-mcp",
        "title": "Test Inference Server Provider for MCP",
        "description": "Inference server provider for MCP discover test",
        "agent": {
          "evaluates": ["throughput", "latency"],
          "target_type": "inference_server",
          "summary": "Evaluates inference server performance"
        },
        "benchmarks": [
          {
            "id": "test_server_benchmark",
            "name": "Server Performance Benchmark"
          }
        ]
      }
      """
    Then the response code should be 201
    When I call MCP tool "discover_providers" with arguments:
      """
      {
        "target_type": "inference_server"
      }
      """
    Then the MCP tool call should succeed
    And the MCP response should contain "test-inference-server-provider-mcp"
    And the MCP response at JSONPath "$.providers[?(@.name==\"test-inference-server-provider-mcp\")].target_type" should equal "inference_server"

  Scenario: Submit evaluation job via MCP with benchmarks
    When I call MCP tool "submit_evaluation" with arguments:
      """
      {
        "name": "mcp_test_job",
        "description": "Test job submitted via MCP",
        "tags": ["mcp", "test"],
        "model": {
          "url": "{{env:MODEL_URL|http://test.com}}",
          "name": "{{env:MODEL_NAME|test}}",
          "auth": {
            "secret_ref": "{{env:MODEL_AUTH_SECRET_REF|test}}"
          }
        },
        "benchmarks": [
          {
            "id": "arc_easy",
            "provider_id": "lm_evaluation_harness"
          }
        ]
      }
      """
    Then the MCP tool call should succeed
    And the MCP response should contain the value "pending" at path "$.state"
  
  Scenario: Submit evaluation job via MCP with collection
    When I call MCP tool "submit_evaluation" with arguments:
      """
      {
        "name": "mcp_collection_job",
        "description": "Test collection job via MCP",
        "model": {
          "url": "{{env:MODEL_URL|http://test.com}}",
          "name": "{{env:MODEL_NAME|test}}",
          "auth": {
            "secret_ref": "{{env:MODEL_AUTH_SECRET_REF|test}}"
          }
        },
        "collection": {
          "id": "leaderboard-v2"
        }
      }
      """
    Then the MCP tool call should succeed
    And the MCP response should contain the value "pending" at path "$.state"

  @mlflow
  Scenario: Submit evaluation job via MCP with MLflow experiment
    When I call MCP tool "submit_evaluation" with arguments:
      """
      {
        "name": "mcp_mlflow_job",
        "description": "Test MLflow tracking via MCP",
        "model": {
          "url": "{{env:MODEL_URL|http://test.com}}",
          "name": "{{env:MODEL_NAME|test}}",
          "auth": {
            "secret_ref": "{{env:MODEL_AUTH_SECRET_REF|test}}"
          }
        },
        "benchmarks": [
          {
            "id": "arc_easy",
            "provider_id": "lm_evaluation_harness"
          }
        ],
        "experiment": {
          "name": "mcp_test_experiment"
        }
      }
      """
    Then the MCP tool call should succeed
    And the MCP response should contain the value "pending" at path "$.state"
  
  Scenario: Get job status via MCP after creating job
    When I call MCP tool "submit_evaluation" with arguments:
      """
      {
        "name": "mcp_status_test_job",
        "description": "Job for testing status endpoint",
        "model": {
          "url": "{{env:MODEL_URL|http://test.com}}",
          "name": "{{env:MODEL_NAME|test}}"
        },
        "benchmarks": [
          {
            "id": "arc_easy",
            "provider_id": "lm_evaluation_harness"
          }
        ]
      }
      """
    Then the MCP tool call should succeed
    And the "job_id" field in the MCP response should be saved as "value:status_job_id"
    When I call MCP tool "get_job_status" with arguments:
      """
      {
        "job_id": "{{value:status_job_id}}"
      }
      """
    Then the MCP tool call should succeed
    And the MCP response should contain the value "{{value:status_job_id}}" at path "$.job_id"
    And the MCP response should contain the value "pending" at path "$.state"
    And the MCP response should contain the value "0" at path "$.progress_percent"
  
  Scenario: Cancel job via MCP
    When I call MCP tool "submit_evaluation" with arguments:
      """
      {
        "name": "mcp_cancel_test_job",
        "description": "Job for testing cancellation",
        "model": {
          "url": "{{env:MODEL_URL|http://test.com}}",
          "name": "{{env:MODEL_NAME|test}}"
        },
        "benchmarks": [
          {
            "id": "arc_easy",
            "provider_id": "lm_evaluation_harness"
          }
        ]
      }
      """
    Then the MCP tool call should succeed
    And the "job_id" field in the MCP response should be saved as "value:cancel_job_id"
    When I call MCP tool "cancel_job" with arguments:
      """
      {
        "job_id": "{{value:cancel_job_id}}"
      }
      """
    Then the MCP tool call should succeed
    When I call MCP tool "get_job_status" with arguments:
      """
      {
        "job_id": "{{value:cancel_job_id}}"
      }
      """
    Then the MCP tool call should succeed
    And the MCP response should contain "cancelled"

  @cluster 
  Scenario: Submit evaluation job via MCP to cluster and wait for completion
    Given the model endpoint is reachable
    And I set the wait interval to "10s"
    When I call MCP tool "submit_evaluation" with arguments:
      """
      {
        "name": "mcp_cluster_test",
        "description": "Cluster test via MCP",
        "model": {
          "url": "{{env:MODEL_URL|http://test.com}}",
          "name": "{{env:MODEL_NAME|test}}",
          "auth": {
            "secret_ref": "{{env:MODEL_AUTH_SECRET_REF|test}}"
          }
        },
        "benchmarks": [
          {
            "id": "arc_easy",
            "provider_id": "lm_evaluation_harness",
            "parameters": {
              "num_fewshot": 0,
              "num_examples": 10
            }
          }
        ]
      }
      """
    Then the MCP tool call should succeed
    And the "job_id" field in the MCP response should be saved as "value:cluster_job_id"
    And I wait for the evaluation job status to be "completed"
    When I call MCP tool "get_job_status" with arguments:
      """
      {
        "job_id": "{{value:cluster_job_id}}"
      }
      """
    Then the MCP tool call should succeed
    And the MCP response should contain the value "completed" at path "$.state"
    And the MCP response should contain the value "100" at path "$.progress_percent"

  @cluster 
  Scenario: Submit evaluation job with collection via MCP to cluster and wait for completion
    Given the model endpoint is reachable
    And I set the wait interval to "10s"
    When I call MCP tool "submit_evaluation" with arguments:
      """
      {
        "name": "mcp_cluster_collection_test",
        "description": "Cluster collection test via MCP",
        "model": {
          "url": "{{env:MODEL_URL|http://test.com}}",
          "name": "{{env:MODEL_NAME|test}}",
          "auth": {
            "secret_ref": "{{env:MODEL_AUTH_SECRET_REF|test}}"
          }
        },
        "collection": {
          "id": "toxicity-and-ethical-principles",
          "benchmarks": [
            {
              "id": "toxigen",
              "provider_id": "lm_evaluation_harness",
              "parameters": {
                "num_examples": 5
              }
            },
            {
              "id": "truthfulqa_mc1",
              "provider_id": "lm_evaluation_harness",
              "parameters": {
                "num_examples": 5
              }
            },
            {
              "id": "bigbench_hhh_alignment_multiple_choice",
              "provider_id": "lm_evaluation_harness",
              "parameters": {
                "num_examples": 5
              }
            }
          ]
        }
      }
      """
    Then the MCP tool call should succeed
    And the "job_id" field in the MCP response should be saved as "value:cluster_collection_job_id"
    And I wait for the evaluation job status to be "completed"
    When I call MCP tool "get_job_status" with arguments:
      """
      {
        "job_id": "{{value:cluster_collection_job_id}}"
      }
      """
    Then the MCP tool call should succeed
    And the MCP response should contain the value "completed" at path "$.state"
    And the MCP response should contain the value "100" at path "$.progress_percent"
  
  @cluster 
  Scenario: Submit evaluation job with multiple benchmarks via MCP to cluster and wait for completion
    Given the model endpoint is reachable
    And I set the wait interval to "10s"
    When I call MCP tool "submit_evaluation" with arguments:
      """
      {
        "name": "mcp_cluster_multi_benchmark_test",
        "description": "Cluster multi-benchmark test via MCP",
        "model": {
          "url": "{{env:MODEL_URL|http://test.com}}",
          "name": "{{env:MODEL_NAME|test}}",
          "auth": {
            "secret_ref": "{{env:MODEL_AUTH_SECRET_REF|test}}"
          }
        },
        "benchmarks": [
          {
            "id": "arc_easy",
            "provider_id": "lm_evaluation_harness",
            "parameters": {
              "num_fewshot": 0,
              "num_examples": 3
            }
          },
          {
            "id": "truthfulqa_mc1",
            "provider_id": "lm_evaluation_harness",
            "parameters": {
              "num_fewshot": 0,
              "num_examples": 3
            }
          }
        ]
      }
      """
    Then the MCP tool call should succeed
    And the "job_id" field in the MCP response should be saved as "value:cluster_multi_job_id"
    And I wait for the evaluation job status to be "completed"
    When I call MCP tool "get_job_status" with arguments:
      """
      {
        "job_id": "{{value:cluster_multi_job_id}}"
      }
      """
    Then the MCP tool call should succeed
    And the MCP response should contain the value "completed" at path "$.state"
    And the MCP response should contain the value "100" at path "$.progress_percent"
 
  @cluster
  @mlflow
  Scenario: Submit evaluation job with MLflow tracking via MCP to cluster and wait for completion
    Given the model endpoint is reachable
    And I set the wait interval to "10s"
    When I call MCP tool "submit_evaluation" with arguments:
      """
      {
        "name": "mcp_cluster_mlflow_test",
        "description": "Cluster MLflow tracking test via MCP",
        "model": {
          "url": "{{env:MODEL_URL|http://test.com}}",
          "name": "{{env:MODEL_NAME|test}}",
          "auth": {
            "secret_ref": "{{env:MODEL_AUTH_SECRET_REF|test}}"
          }
        },
        "benchmarks": [
          {
            "id": "arc_easy",
            "provider_id": "lm_evaluation_harness",
            "parameters": {
              "num_fewshot": 0,
              "num_examples": 5
            }
          }
        ],
        "experiment": {
          "name": "mcp_cluster_mlflow_experiment"
        }
      }
      """
    Then the MCP tool call should succeed
    And the "job_id" field in the MCP response should be saved as "value:cluster_mlflow_job_id"
    And I wait for the evaluation job status to be "completed"
    When I call MCP tool "get_job_status" with arguments:
      """
      {
        "job_id": "{{value:cluster_mlflow_job_id}}"
      }
      """
    Then the MCP tool call should succeed
    And the MCP response should contain the value "completed" at path "$.state"
    And the MCP response should contain the value "100" at path "$.progress_percent"

  @cluster 
  Scenario: Verify benchmark results and metrics via MCP resource after cluster job completion
    Given the model endpoint is reachable
    And I set the wait interval to "10s"
    When I call MCP tool "submit_evaluation" with arguments:
      """
      {
        "name": "mcp_cluster_results_validation",
        "description": "Cluster test to validate benchmark results via MCP resource",
        "model": {
          "url": "{{env:MODEL_URL|http://test.com}}",
          "name": "{{env:MODEL_NAME|test}}",
          "auth": {
            "secret_ref": "{{env:MODEL_AUTH_SECRET_REF|test}}"
          }
        },
        "benchmarks": [
          {
            "id": "arc_easy",
            "provider_id": "lm_evaluation_harness",
            "parameters": {
              "num_fewshot": 0,
              "num_examples": 5
            }
          }
        ]
      }
      """
    Then the MCP tool call should succeed
    And the "job_id" field in the MCP response should be saved as "value:cluster_results_job_id"
    And I wait for the evaluation job status to be "completed"
    When I read MCP resource "evalhub://jobs/{{value:cluster_results_job_id}}"
    Then the MCP resource read should succeed
    And the MCP resource should contain the value "completed" at path "$.status.state"
    And the MCP resource should contain the value "arc_easy" at path "$.results.benchmarks[0].id"
    And the MCP resource should contain the value "lm_evaluation_harness" at path "$.results.benchmarks[0].provider_id"
    And the MCP resource should contain "metrics"
    And the MCP resource should contain the value "{{env:MODEL_NAME|test}}" at path "$.model.name"
    And the MCP resource should contain the value "{{env:MODEL_URL|http://test.com}}" at path "$.model.url"

#---- MCP Resources ----
  Scenario: Read providers resource via MCP
    When I read MCP resource "evalhub://providers"
    Then the MCP resource read should succeed
    And the MCP resource should contain "lm_evaluation_harness"
    And the MCP resource should contain "lighteval"

  Scenario: Read specific provider resource by ID via MCP
    When I read MCP resource "evalhub://providers/lm_evaluation_harness"
    Then the MCP resource read should succeed
    And the MCP resource should contain the value "lm_evaluation_harness" at path "$.resource.id"
    And the MCP resource should contain the value "LM Evaluation Harness" at path "$.name"
  
  Scenario: Read benchmarks resource via MCP includes agent metadata
    When I read MCP resource "evalhub://benchmarks"
    Then the MCP resource read should succeed
    And the MCP resource should contain "arc_easy"
    And the MCP resource should contain "hellaswag"
    And the MCP resource should contain "agent"
    And the MCP resource should contain "result_interpretation"
  
  Scenario: Read benchmarks filtered by label via MCP
    When I read MCP resource "evalhub://benchmarks?label=reasoning"
    Then the MCP resource read should succeed
    And the MCP resource should contain "arc_easy"

  Scenario: Read specific benchmark resource by ID via MCP includes agent metadata
    When I read MCP resource "evalhub://benchmarks/arc_easy"
    Then the MCP resource read should succeed
    And the MCP resource should contain the value "arc_easy" at path "$.id"
    And the MCP resource should contain the value "reasoning" at path "$.category"
    And the MCP resource should contain "agent"
    And the MCP resource should contain "result_interpretation"
    And the MCP resource should contain "score_ranges"

  Scenario: Read collections resource via MCP
    When I read MCP resource "evalhub://collections"
    Then the MCP resource read should succeed
    And the MCP resource should contain "leaderboard-v2"

  Scenario: Read specific collection resource by ID via MCP
    When I read MCP resource "evalhub://collections/leaderboard-v2"
    Then the MCP resource read should succeed
    And the MCP resource should contain the value "leaderboard-v2" at path "$.resource.id"

  Scenario: Read jobs resource after creating job via MCP
    When I call MCP tool "submit_evaluation" with arguments:
      """
      {
        "name": "mcp_resource_test_job",
        "model": {
          "url": "{{env:MODEL_URL|http://test.com}}",
          "name": "{{env:MODEL_NAME|test}}"
        },
        "benchmarks": [
          {
            "id": "arc_easy",
            "provider_id": "lm_evaluation_harness"
          }
        ]
      }
      """
    Then the MCP tool call should succeed
    And the "job_id" field in the MCP response should be saved as "value:resource_job_id"
    When I read MCP resource "evalhub://jobs"
    Then the MCP resource read should succeed
    And the MCP resource should contain "{{value:resource_job_id}}"
    When I read MCP resource "evalhub://jobs/{{value:resource_job_id}}"
    Then the MCP resource read should succeed
    And the MCP resource should contain the value "{{value:resource_job_id}}" at path "$.resource.id"
    And the MCP resource should contain the value "pending" at path "$.status.state"

  @negative
  Scenario: Read non-existent provider resource fails
    When I read MCP resource "evalhub://providers/non-existent-provider-xyz"
    Then the MCP resource read should fail
    And the MCP resource error should contain "not found"

  @negative
  Scenario: Read non-existent benchmark resource fails
    When I read MCP resource "evalhub://benchmarks/non-existent-benchmark-xyz"
    Then the MCP resource read should fail
    And the MCP resource error should contain "not found"

#---- MCP Prompts ----
  Scenario: Get edd_workflow prompt via MCP
    When I get MCP prompt "edd_workflow" with arguments:
      """
      {
        "application_type": "rag"
      }
      """
    Then the MCP prompt should succeed
    And the MCP prompt should contain "evaluation"
    And the MCP prompt should contain "rag"

  Scenario: Get evaluate_model prompt via MCP
    When I get MCP prompt "evaluate_model" with arguments:
      """
      {
        "model_name": "test-model",
        "model_url": "http://test.com"
      }
      """
    Then the MCP prompt should succeed
    And the MCP prompt should contain "http://test.com"
    And the MCP prompt should contain "evaluate"

  Scenario: Get compare_runs prompt via MCP
    When I get MCP prompt "compare_runs" with arguments:
      """
      {
        "job_ids": "job1,job2"
      }
      """
    Then the MCP prompt should succeed
    And the MCP prompt should contain "compare"

  @negative
  Scenario: Get edd_workflow prompt with invalid application_type fails
    When I get MCP prompt "edd_workflow" with arguments:
      """
      {
        "application_type": "invalid_type"
      }
      """
    Then the MCP prompt should fail
    And the MCP prompt error should contain "application_type"

#----
  @negative
  Scenario: Submit evaluation job missing required name fails
    When I call MCP tool "submit_evaluation" with arguments:
      """
      {
        "model": {
          "url": "{{env:MODEL_URL|http://test.com}}",
          "name": "{{env:MODEL_NAME|test}}"
        },
        "benchmarks": [
          {
            "id": "arc_easy",
            "provider_id": "lm_evaluation_harness"
          }
        ]
      }
      """
    Then the MCP tool call should fail
    And the MCP error should contain "missing properties"
    And the MCP error should contain "name"

  @negative
  Scenario: Submit evaluation job missing model fails
    When I call MCP tool "submit_evaluation" with arguments:
      """
      {
        "name": "invalid_job",
        "benchmarks": [
          {
            "id": "arc_easy",
            "provider_id": "lm_evaluation_harness"
          }
        ]
      }
      """
    Then the MCP tool call should fail
    And the MCP error should contain "missing properties"
    And the MCP error should contain "model"

  @negative
  Scenario: Submit evaluation job with both benchmarks and collection fails
    When I call MCP tool "submit_evaluation" with arguments:
      """
      {
        "name": "invalid_job",
        "model": {
          "url": "{{env:MODEL_URL|http://test.com}}",
          "name": "{{env:MODEL_NAME|test}}"
        },
        "benchmarks": [
          {
            "id": "arc_easy",
            "provider_id": "lm_evaluation_harness"
          }
        ],
        "collection": {
          "id": "leaderboard-v2"
        }
      }
      """
    Then the MCP tool call should fail
    And the MCP error should contain "validation error: provide 'benchmarks' or 'collection', not both"

  @negative
  Scenario: Submit evaluation job with neither benchmarks nor collection fails
    When I call MCP tool "submit_evaluation" with arguments:
      """
      {
        "name": "invalid_job",
        "model": {
          "url": "{{env:MODEL_URL|http://test.com}}",
          "name": "{{env:MODEL_NAME|test}}"
        }
      }
      """
    Then the MCP tool call should fail
    And the MCP error should contain "validation error: provide at least one of 'benchmarks' or 'collection'"

  @negative
  Scenario: Get status for non-existent job fails
    When I call MCP tool "get_job_status" with arguments:
      """
      {
        "job_id": "non-existent-job-id-12345"
      }
      """
    Then the MCP tool call should fail
    And the MCP error should contain "resource_not_found"

  @negative
  Scenario: Cancel non-existent job fails
    When I call MCP tool "cancel_job" with arguments:
      """
      {
        "job_id": "non-existent-job-id-67890"
      }
      """
    Then the MCP tool call should fail
    And the MCP error should contain "resource_not_found"
