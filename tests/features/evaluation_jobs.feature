@cluster
@evaluations
Feature: Evaluation Jobs
  As a data scientist
  I want to create evaluation jobs against a running cluster
  So that I evaluate models

  Background:
    Given I set the header "X-Tenant" to "{{env:X_TENANT|test-tenant}}"
    And I set the header "X-User" to "{{env:X_USER|test-user}}"
    And I set the wait deadline to "{{env:WAIT_DEADLINE|30m}}"
    And the model endpoint is reachable
    # This is mandatory for the tests to run successfully
    And the value "{{env:MODEL_AUTH_SECRET_REF}}" is not empty

  @connected
  Scenario: Check evaluation job creation on a connected cluster
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job.json"
    Then the response code should be 202
    And the response should contain the value "{{env:X_TENANT|test-tenant}}" at path "$.resource.tenant"
    And the response should have schema from file "file:/schemas/evaluation_job_resource_connected.schema.json"

  @disconnected
  Scenario: Check evaluation job creation on a disconnected cluster
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job.json"
    Then the response code should be 202
    And the response should contain the value "{{env:X_TENANT|test-tenant}}" at path "$.resource.tenant"
    And the response should have schema from file "file:/schemas/evaluation_job_resource_disconnected.schema.json"

  Scenario: Verifying results returned for Evaluation job
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job.json"
    Then the response code should be 202
    And the response should contain the value "pending" at path "$.status.state"
    And the response should contain the value "evaluation_job_created" at path "$.status.message.message_code"
    And I wait for the evaluation job status to be "completed"
    When I send a GET request to "/api/v1/evaluations/jobs/{id}"
    Then the response code should be 200
    And the response should contain the value "completed" at path "$.status.state"
    And the response should contain the value "completed" at path "$.status.benchmarks[0].status"
    And the response should contain the value "arc_easy" at path "$.status.benchmarks[0].id"
    And the response should contain the value "lm_evaluation_harness" at path "$.status.benchmarks[0].provider_id"
    And the response should contain "results"
    And the array at path "results.benchmarks" in the response should have length 1
    And the response should contain the value "arc_easy" at path "$.results.benchmarks[0].id"
    And the response should contain the value "lm_evaluation_harness" at path "$.results.benchmarks[0].provider_id"
    And the response should contain at least the value "0.2" at path "$.results.benchmarks[0].metrics.acc"
    And the response should contain at least the value "0.4" at path "$.results.benchmarks[0].metrics.acc_norm"
    And the response should contain the value "{{env:MODEL_NAME|test}}" at path "$.model.name"
    And the response should contain the value "{{env:MODEL_URL|http://test.com}}" at path "$.model.url"
    And the response should contain the value "test-evaluation-job" at path "$.name"
    And the response should contain the value "10" at path "$.benchmarks[0].parameters.num_examples"
    And the response should contain the value "{{env:FVT_BENCHMARK_TOKENIZER|google/flan-t5-small}}" at path "$.benchmarks[0].parameters.tokenizer"
    And the response should not contain the value "collection" at path "$.collection"
    When I send a DELETE request to "/api/v1/evaluations/jobs/{id}?hard_delete=true"
    Then the response code should be 204

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
    And the response should contain the value "{{env:FVT_BENCHMARK_TOKENIZER|google/flan-t5-small}}" at path "$.benchmarks[0].parameters.tokenizer"
    When I send a GET request to "/api/v1/evaluations/jobs/{id}"
    Then the response code should be 200
    And the response should contain the value "pending" at path "$.status.state"
    And the response should contain the value "{{env:MODEL_NAME|test}}" at path "$.model.name"
    And the response should contain the value "{{env:MODEL_URL|http://test.com}}" at path "$.model.url"
    And the response should contain the value "arc_easy" at path "$.benchmarks[0].id"
    And the response should contain the value "garak|lm_evaluation_harness" at path "$.benchmarks[0].provider_id"
    And the response should contain the value "3" at path "$.benchmarks[0].parameters.num_fewshot"
    And the response should contain the value "5" at path "$.benchmarks[0].parameters.limit"
    And the response should contain the value "{{env:FVT_BENCHMARK_TOKENIZER|google/flan-t5-small}}" at path "$.benchmarks[0].parameters.tokenizer"
    And the response should not contain the value "collection" at path "$.collection"
    And I wait for the evaluation job status to be "completed"
    When I send a DELETE request to "/api/v1/evaluations/jobs/{id}?hard_delete=true"
    Then the response code should be 204
    When I send a GET request to "/api/v1/evaluations/jobs/{id}"
    Then the response code should be 404
    And the response should contain the value "resource_not_found" at path "$.message_code"

  @mlflow
  Scenario: Create an evaluation job with MLflow experiment
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job.json"
    Then the response code should be 202
    And the response should contain the value "evaluation_job_created" at path "$.status.message.message_code"
    And the response should contain the value "environment" at path "$.experiment.tags[0].key"
    And the response should contain the value "test" at path "$.experiment.tags[0].value"
    When I send a GET request to "/api/v1/evaluations/jobs/{id}"
    Then the response code should be 200
    And the response should contain the value "environment" at path "$.experiment.tags[0].key"
    And the response should contain the value "test" at path "$.experiment.tags[0].value"
    When I send a DELETE request to "/api/v1/evaluations/jobs/{id}?hard_delete=true"
    Then the response code should be 204

  Scenario: Evaluation job with multiple benchmarks from same provider
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job_multiple_benchmark.json"
    Then the response code should be 202
    And the response should contain the value "pending" at path "$.status.state"
    And the response should contain the value "evaluation_job_created" at path "$.status.message.message_code"
    And I wait for the evaluation job status to be "completed"
    When I send a GET request to "/api/v1/evaluations/jobs/{id}"
    Then the response code should be 200
    And the response should contain the value "completed" at path "$.status.state"
    And the array at path "results.benchmarks" in the response should have length 2
    And the response should contain the value "completed" at path "$.status.benchmarks[0].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[1].status"
    And the response should contain at least the value "0.2" at path "$.results.benchmarks[0].metrics.acc"
    And the response should contain at least the value "0.4" at path "$.results.benchmarks[0].metrics.acc_norm"
    And the response should contain at least the value "0.2" at path "$.results.benchmarks[1].metrics.acc"
    And the response should contain at least the value "0.4" at path "$.results.benchmarks[1].metrics.acc_norm"
    And the response should contain the value "arc_easy" at path "$.benchmarks[0].id"
    And the response should contain the value "5" at path "$.benchmarks[0].parameters.num_examples"
    And the response should contain the value "arc_easy" at path "$.benchmarks[1].id"
    And the response should contain the value "10" at path "$.benchmarks[1].parameters.num_examples"
    And the response should not contain the value "collection" at path "$.collection"
    When I send a DELETE request to "/api/v1/evaluations/jobs/{id}?hard_delete=true"
    Then the response code should be 204

  Scenario: Multiple jobs can be submitted
    Given the service is running
    And I set the header "X-User" to "test-user-1"
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job.json"
    Then the response code should be 202
    And the "resource.id" field in the response should be saved as "value:job1_id"
    And I set the header "X-User" to "test-user-2"
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job.json"
    Then the response code should be 202
    And the "resource.id" field in the response should be saved as "value:job2_id"
    And I set the header "X-User" to "test-user-3"
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job.json"
    Then the response code should be 202
    And the "resource.id" field in the response should be saved as "value:job3_id"
    When I send a GET request to "/api/v1/evaluations/jobs/{{value:job1_id}}"
    Then the response code should be 200
    When I send a GET request to "/api/v1/evaluations/jobs/{{value:job2_id}}"
    Then the response code should be 200
    When I send a GET request to "/api/v1/evaluations/jobs/{{value:job3_id}}"
    Then the response code should be 200
    When I send a DELETE request to "/api/v1/evaluations/jobs/{{value:job1_id}}?hard_delete=true"
    Then the response code should be 204
    When I send a DELETE request to "/api/v1/evaluations/jobs/{{value:job2_id}}?hard_delete=true"
    Then the response code should be 204
    When I send a DELETE request to "/api/v1/evaluations/jobs/{{value:job3_id}}?hard_delete=true"
    Then the response code should be 204

  @mlflow
  Scenario: Multiple jobs share same MLflow experiment
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body:
      """
      {
        "name": "automation_shared_experiment_job_1",
        "model": {
          "url": "{{env:MODEL_URL|http://test.com}}",
          "name": "{{env:MODEL_NAME|test}}"
        },
        "benchmarks": [
          {
            "id": "arc_easy",
            "provider_id": "lm_evaluation_harness",
            "parameters": {
              "num_examples": 3,
              "tokenizer": "google/flan-t5-small"
            }
          }
        ],
        "experiment": {
          "name": "automation_shared_experiment"
        }
      }
      """
    Then the response code should be 202
    And the "resource.mlflow_experiment_id" field in the response should be saved as "value:exp_id"
    And the "resource.id" field in the response should be saved as "value:exp_job1_id"
    When I send a POST request to "/api/v1/evaluations/jobs" with body:
      """
      {
        "name": "automation_shared_experiment_job_2",
        "model": {
          "url": "{{env:MODEL_URL|http://test.com}}",
          "name": "{{env:MODEL_NAME|test}}"
        },
        "benchmarks": [
          {
            "id": "arc_easy",
            "provider_id": "lm_evaluation_harness",
            "parameters": {
              "num_examples": 3,
              "tokenizer": "google/flan-t5-small"
            }
          }
        ],
        "experiment": {
          "name": "automation_shared_experiment"
        }
      }
      """
    Then the response code should be 202
    And the response should contain the value "{{value:exp_id}}" at path "$.resource.mlflow_experiment_id"
    And the "resource.id" field in the response should be saved as "value:exp_job2_id"
    When I send a DELETE request to "/api/v1/evaluations/jobs/{{value:exp_job1_id}}?hard_delete=true"
    Then the response code should be 204
    When I send a DELETE request to "/api/v1/evaluations/jobs/{{value:exp_job2_id}}?hard_delete=true"
    Then the response code should be 204

  Scenario: Collection job completes successfully
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/collections" with body "file:/collection.json"
    Then the response code should be 201
    And the "resource.id" field in the response should be saved as "value:collection_id"
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job.json"
    Then the response code should be 202
    And the response should contain the value "evaluation_job_created" at path "$.status.message.message_code"
    And the response should contain the value "pending" at path "$.status.state"
    And I wait for the evaluation job status to be "completed"
    When I send a GET request to "/api/v1/evaluations/jobs/{id}"
    Then the response code should be 200
    And the response should contain the value "completed" at path "$.status.state"
    And the response should contain "results"
    And the response should contain the value "{{value:collection_id}}" at path "$.collection.id"
    When I send a DELETE request to "/api/v1/evaluations/jobs/{id}?hard_delete=true"
    Then the response code should be 204
    When I send a DELETE request to "/api/v1/evaluations/collections/{{value:collection_id}}?hard_delete=true"
    Then the response code should be 204

  Scenario: Evaluation job completes with multi-benchmark collection
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/collections" with body "file:/collection_multi_benchmark.json"
    Then the response code should be 201
    And the "resource.id" field in the response should be saved as "value:collection_id"
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job.json"
    Then the response code should be 202
    And I wait for the evaluation job status to be "completed"
    When I send a GET request to "/api/v1/evaluations/jobs/{id}"
    Then the response code should be 200
    And the response should contain the value "completed" at path "$.status.state"
    And the response should contain "results"
    And the array at path "results.benchmarks" in the response should have length 2
    And the response should contain the value "{{value:collection_id}}" at path "$.collection.id"
    And the response should contain the value "completed" at path "$.status.benchmarks[0].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[1].status"
    And the response should contain at least the value "0.2" at path "$.results.benchmarks[0].metrics.acc"
    And the response should contain at least the value "0.4" at path "$.results.benchmarks[0].metrics.acc_norm"
    And the response should contain at least the value "0.2" at path "$.results.benchmarks[1].metrics.acc"
    And the response should contain at least the value "0.4" at path "$.results.benchmarks[1].metrics.acc_norm"
    When I send a DELETE request to "/api/v1/evaluations/jobs/{id}?hard_delete=true"
    Then the response code should be 204
    When I send a DELETE request to "/api/v1/evaluations/collections/{{value:collection_id}}?hard_delete=true"
    Then the response code should be 204

  Scenario: Multiple jobs sharing same collection can be submitted
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/collections" with body "file:/collection.json"
    Then the response code should be 201
    And the "resource.id" field in the response should be saved as "value:collection_id"
    And I set the header "X-User" to "test-user-1"
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job.json"
    Then the response code should be 202
    And the "resource.id" field in the response should be saved as "value:job1_id"
    And I set the header "X-User" to "test-user-2"
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job.json"
    Then the response code should be 202
    And the "resource.id" field in the response should be saved as "value:job2_id"
    When I send a GET request to "/api/v1/evaluations/jobs/{{value:job1_id}}"
    Then the response code should be 200
    And the response should contain the value "{{value:collection_id}}" at path "$.collection.id"
    When I send a GET request to "/api/v1/evaluations/jobs/{{value:job2_id}}"
    Then the response code should be 200
    And the response should contain the value "{{value:collection_id}}" at path "$.collection.id"
    When I send a DELETE request to "/api/v1/evaluations/jobs/{{value:job1_id}}?hard_delete=true"
    Then the response code should be 204
    When I send a DELETE request to "/api/v1/evaluations/jobs/{{value:job2_id}}?hard_delete=true"
    Then the response code should be 204
    When I send a DELETE request to "/api/v1/evaluations/collections/{{value:collection_id}}?hard_delete=true"
    Then the response code should be 204

  @mlflow
  Scenario: Collection jobs share same MLflow experiments
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/collections" with body "file:/collection.json"
    Then the response code should be 201
    And the "resource.id" field in the response should be saved as "value:collection_id"
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job_mlflow_collection_shared_job1.jsonnet"
    Then the response code should be 202
    And the "resource.mlflow_experiment_id" field in the response should be saved as "value:exp_id"
    And the "resource.id" field in the response should be saved as "value:exp_job1_id"
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job_mlflow_collection_shared_job2.jsonnet"
    Then the response code should be 202
    And the response should contain the value "{{value:exp_id}}" at path "$.resource.mlflow_experiment_id"
    And the "resource.id" field in the response should be saved as "value:exp_job2_id"
    When I send a GET request to "/api/v1/evaluations/jobs/{{value:exp_job1_id}}"
    Then the response code should be 200
    And the response should contain the value "{{value:collection_id}}" at path "$.collection.id"
    And the response should contain the value "{{value:exp_id}}" at path "$.resource.mlflow_experiment_id"
    When I send a GET request to "/api/v1/evaluations/jobs/{{value:exp_job2_id}}"
    Then the response code should be 200
    And the response should contain the value "{{value:collection_id}}" at path "$.collection.id"
    And the response should contain the value "{{value:exp_id}}" at path "$.resource.mlflow_experiment_id"
    When I send a DELETE request to "/api/v1/evaluations/jobs/{{value:exp_job1_id}}?hard_delete=true"
    Then the response code should be 204
    When I send a DELETE request to "/api/v1/evaluations/jobs/{{value:exp_job2_id}}?hard_delete=true"
    Then the response code should be 204
    When I send a DELETE request to "/api/v1/evaluations/collections/{{value:collection_id}}?hard_delete=true"
    Then the response code should be 204

  Scenario: Collection job with parameters
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/collections" with body "file:/collection_job_parameters.json"
    Then the response code should be 201
    And the "resource.id" field in the response should be saved as "value:collection_id"
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job.json"
    Then the response code should be 202
    And the response should contain the value "evaluation_job_created" at path "$.status.message.message_code"
    And the response should contain the value "pending" at path "$.status.state"
    And I wait for the evaluation job status to be "completed"
    When I send a GET request to "/api/v1/evaluations/collections/{{value:collection_id}}"
    Then the response code should be 200
    And the response should contain the value "3" at path "$.benchmarks[0].parameters.num_examples"
    And the response should contain the value "{{env:FVT_BENCHMARK_TOKENIZER|google/flan-t5-small}}" at path "$.benchmarks[0].parameters.tokenizer"
    When I send a GET request to "/api/v1/evaluations/jobs/{id}"
    Then the response code should be 200
    And the response should contain the value "completed" at path "$.status.state"
    And the response should contain "results"
    And the response should contain the value "{{value:collection_id}}" at path "$.collection.id"
    When I send a DELETE request to "/api/v1/evaluations/jobs/{id}?hard_delete=true"
    Then the response code should be 204
    When I send a DELETE request to "/api/v1/evaluations/collections/{{value:collection_id}}?hard_delete=true"
    Then the response code should be 204

  Scenario: Create threshold-zero collection then submit job and verify completion
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/collections" with body "file:/collection_threshold_zero.json"
    Then the response code should be 201
    And the "resource.id" field in the response should be saved as "value:collection_id"
    And the response should contain the value "test-benchmarks-collection-threshold-zero" at path "$.name"
    And the response should contain the value "test" at path "$.category"
    And the response should contain the value "Collection of benchmarks for FVT" at path "$.description"
    And the response should contain the value "0" at path "$.pass_criteria.threshold"
    And the array at path "$.benchmarks" in the response should have length 2
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job.json"
    Then the response code should be 202
    And the response should contain the value "evaluation_job_created" at path "$.status.message.message_code"
    And the response should contain the value "pending" at path "$.status.state"
    And I wait for the evaluation job status to be "completed"
    When I send a GET request to "/api/v1/evaluations/jobs/{id}"
    Then the response code should be 200
    And the response should contain the value "completed" at path "$.status.state"
    And the response should contain "results"
    And the response should contain the value "{{value:collection_id}}" at path "$.collection.id"

  Scenario: Create evaluation job with Collection
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/collections" with body "file:/collection.json"
    Then the response code should be 201
    And the "resource.id" field in the response should be saved as "value:collection_id"
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job.json"
    Then the response code should be 202
    And the response should contain the value "evaluation_job_created" at path "$.status.message.message_code"
    And the response should contain the value "pending" at path "$.status.state"
    When I send a GET request to "/api/v1/evaluations/jobs/{id}"
    Then the response code should be 200
    And the response should contain the value "{{value:collection_id}}" at path "$.collection.id"
    And I wait for the evaluation job status to be "completed"
    When I send a DELETE request to "/api/v1/evaluations/jobs/{id}?hard_delete=true"
    Then the response code should be 204
    When I send a DELETE request to "/api/v1/evaluations/collections/{{value:collection_id}}"
    Then the response code should be 204

  Scenario: Create an evaluation job and wait for completion
    Given the service is running
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

 # This test uses gated HuggingFace datasets (toxigen, truthfulqa_mc1, bigbench_hhh_alignment_multiple_choice) which require authentication.
  Scenario: Verify Evaluation Jobs Can Use OOB Collections - toxicity-and-ethical-principles
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job_oob_toxicity.jsonnet"
    Then the response code should be 202
    And the response should contain the value "toxicity-and-ethical-principles" at path "$.collection.id"
    And I wait for the evaluation job status to be "completed"
    When I send a GET request to "/api/v1/evaluations/jobs/{id}"
    Then the response code should be 200
    And the response should contain the value "toxicity-and-ethical-principles" at path "$.collection.id"
    And the response should contain the value "Evaluation job is completed" at path "$.status.message.message"
    And the response should contain the value "completed" at path "$.status.benchmarks[0].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[1].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[2].status"
    And the array at path "results.benchmarks" in the response should have length 3
    And the response should contain the value "toxigen" at path "$.results.benchmarks[*].id"
    And the response should contain the value "truthfulqa_mc1" at path "$.results.benchmarks[*].id"
    And the response should contain the value "bigbench_hhh_alignment_multiple_choice" at path "$.results.benchmarks[*].id"
    And the response should equal the value "5" at path "$.collection.benchmarks[0].parameters.num_examples"
    And the response should equal the value "5" at path "$.collection.benchmarks[1].parameters.num_examples"
    And the response should equal the value "5" at path "$.collection.benchmarks[2].parameters.num_examples"
    And the response should contain "results"
    # TODO: Add specific metric validations once we verify actual response structure

  # This scenario requires HuggingFace authentication for all benchmarks to run
  @slow
  Scenario: Verify Evaluation Jobs Can Use OOB Collections - leaderboard-v2
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body:
      """
      {
        "name": "test-evaluation-job-oob-collection",
        "collection": {
          "id": "leaderboard-v2",
          "benchmarks": [
            {
              "id": "leaderboard_ifeval",
              "provider_id": "lm_evaluation_harness",
              "parameters": {
                "limit": 1
              }
            },
            {
              "id": "leaderboard_bbh",
              "provider_id": "lm_evaluation_harness",
              "parameters": {
                "limit": 2
              }
            },
            {
              "id": "leaderboard_gpqa",
              "provider_id": "lm_evaluation_harness",
              "parameters": {
                "limit": 1
              }
            },
            {
              "id": "leaderboard_mmlu_pro",
              "provider_id": "lm_evaluation_harness",
              "parameters": {
                "limit": 2
              }
            },
            {
              "id": "leaderboard_musr",
              "provider_id": "lm_evaluation_harness",
              "parameters": {
                "limit": 1
              }
            },
            {
              "id": "leaderboard_math_hard",
              "provider_id": "lm_evaluation_harness",
              "parameters": {
                "limit": 2
              }
            }
          ]
        },
        "model": {
          "url": "{{env:MODEL_URL|http://test.com}}",
          "name": "{{env:MODEL_NAME|test}}",
          "auth": {
            "secret_ref": "{{env:MODEL_AUTH_SECRET_REF|test}}"
          }
        }
      }
      """
    Then the response code should be 202
    And the response should contain the value "leaderboard-v2" at path "$.collection.id"
    And I wait for the evaluation job status to be "completed"
    When I send a GET request to "/api/v1/evaluations/jobs/{id}"
    Then the response code should be 200
    And the response should contain the value "leaderboard-v2" at path "$.collection.id"
    And the response should contain the value "Evaluation job is completed" at path "$.status.message.message"
    And the response should contain the value "completed" at path "$.status.benchmarks[0].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[1].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[2].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[3].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[4].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[5].status"
    And the array at path "results.benchmarks" in the response should have length 6
    And the response should contain the value "leaderboard_ifeval" at path "$.results.benchmarks[*].id"
    And the response should contain the value "leaderboard_bbh" at path "$.results.benchmarks[*].id"
    And the response should contain the value "leaderboard_gpqa" at path "$.results.benchmarks[*].id"
    And the response should contain the value "leaderboard_mmlu_pro" at path "$.results.benchmarks[*].id"
    And the response should contain the value "leaderboard_musr" at path "$.results.benchmarks[*].id"
    And the response should contain the value "leaderboard_math_hard" at path "$.results.benchmarks[*].id"
    And the response should equal the value "1" at path "$.collection.benchmarks[0].parameters.limit"
    And the response should equal the value "2" at path "$.collection.benchmarks[1].parameters.limit"
    And the response should equal the value "1" at path "$.collection.benchmarks[2].parameters.limit"
    And the response should equal the value "2" at path "$.collection.benchmarks[3].parameters.limit"
    And the response should equal the value "1" at path "$.collection.benchmarks[4].parameters.limit"
    And the response should equal the value "2" at path "$.collection.benchmarks[5].parameters.limit"
    # TODO: Add specific metric validations once we verify actual response structure

  # This scenario requires HuggingFace authentication for all benchmarks to run
  @slow
  Scenario: Verify Evaluation Jobs Can Use OOB Collections - safety-and-fairness-v1
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body:
      """
      {
        "name": "test-evaluation-job-oob-collection",
        "collection": {
          "id": "safety-and-fairness-v1",
          "benchmarks": [
            {
              "id": "truthfulqa_mc1",
              "provider_id": "lm_evaluation_harness",
              "parameters": {
                "limit": 5
              }
            },
            {
              "id": "toxigen",
              "provider_id": "lm_evaluation_harness",
              "parameters": {
                "limit": 3
              }
            },
            {
              "id": "winogender",
              "provider_id": "lm_evaluation_harness",
              "parameters": {
                "limit": 1
              }
            },
            {
              "id": "crows_pairs_english",
              "provider_id": "lm_evaluation_harness",
              "parameters": {
                "limit": 4
              }
            },
            {
              "id": "bbq",
              "provider_id": "lm_evaluation_harness",
              "parameters": {
                "limit": 2
              }
            },
            {
              "id": "ethics_cm",
              "provider_id": "lm_evaluation_harness",
              "parameters": {
                "limit": 6
              }
            }
          ]
        },
        "model": {
          "url": "{{env:MODEL_URL|http://test.com}}",
          "name": "{{env:MODEL_NAME|test}}",
          "auth": {
            "secret_ref": "{{env:MODEL_AUTH_SECRET_REF|test}}"
          }
        }
      }
      """
    Then the response code should be 202
    And the response should contain the value "safety-and-fairness-v1" at path "$.collection.id"
    And I wait for the evaluation job status to be "completed"
    When I send a GET request to "/api/v1/evaluations/jobs/{id}"
    Then the response code should be 200
    And the response should contain the value "safety-and-fairness-v1" at path "$.collection.id"
    And the response should contain the value "Evaluation job is completed" at path "$.status.message.message"
    And the response should contain the value "completed" at path "$.status.benchmarks[0].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[1].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[2].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[3].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[4].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[5].status"
    And the array at path "results.benchmarks" in the response should have length 6
    And the response should contain the value "toxigen" at path "$.results.benchmarks[*].id"
    And the response should contain the value "truthfulqa_mc1" at path "$.results.benchmarks[*].id"
    And the response should contain the value "winogender" at path "$.results.benchmarks[*].id"
    And the response should contain the value "crows_pairs_english" at path "$.results.benchmarks[*].id"
    And the response should contain the value "bbq" at path "$.results.benchmarks[*].id"
    And the response should contain the value "ethics_cm" at path "$.results.benchmarks[*].id"
    And the response should equal the value "5" at path "$.collection.benchmarks[0].parameters.limit"
    And the response should equal the value "3" at path "$.collection.benchmarks[1].parameters.limit"
    And the response should equal the value "1" at path "$.collection.benchmarks[2].parameters.limit"
    And the response should equal the value "4" at path "$.collection.benchmarks[3].parameters.limit"
    And the response should equal the value "2" at path "$.collection.benchmarks[4].parameters.limit"
    And the response should equal the value "6" at path "$.collection.benchmarks[5].parameters.limit"
  # TODO: Add specific metric validations once we verify actual response structure

  @negative
  Scenario: Verify Evaluation Jobs cannot be created using non existant OOB Collections
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body:
      """
      {
        "name": "test-evaluation-job-oob-collection",
        "collection": {
          "id": "non-existant-collection"
        },
        "model": {
          "url": "{{env:MODEL_URL|http://test.com}}",
          "name": "{{env:MODEL_NAME|test}}"
        }
      }
      """
    Then the response code should be 404
    And the response should contain the value "resource_not_found" at path "$.message_code"
    And the response should contain the value "was not found." at path "$.message"

  Scenario: Multiple jobs can reference different OOB collection
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job_oob_ref_safety_fairness_job1.jsonnet"
    Then the response code should be 202
    And the response should contain the value "safety-and-fairness-v1" at path "$.collection.id"
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job_oob_ref_toxicity_job2.jsonnet"
    Then the response code should be 202
    And the response should contain the value "toxicity-and-ethical-principles" at path "$.collection.id"
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job_oob_ref_leaderboard_job3.jsonnet"
    Then the response code should be 202
    And the response should contain the value "leaderboard-v2" at path "$.collection.id"

  Scenario: Multiple jobs can reference same OOB collection
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job_oob_ref_safety_same1.jsonnet"
    Then the response code should be 202
    And the response should contain the value "safety-and-fairness-v1" at path "$.collection.id"
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job_oob_ref_safety_same2.jsonnet"
    Then the response code should be 202
    And the response should contain the value "safety-and-fairness-v1" at path "$.collection.id"

  @mlflow
  Scenario: Create an evaluation job with MLflow experiment and OOB collection
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job_mlflow_oob_leaderboard.jsonnet"
    Then the response code should be 202
    When I send a GET request to "/api/v1/evaluations/jobs/{id}"
    Then the response code should be 200
    And the response should contain the value "leaderboard-v2" at path "$.collection.id"
    And the response should match the value "^[0-9]+$" at path "$.resource.mlflow_experiment_id"
    # And the response should match the value "^[0-9a-z:/.-_]+/api/2.0/mlflow/experiments$" at path "$.results.mlflow_experiment_url"

  @negative
  Scenario: Verify Evaluation Jobs cannot use both benchmarks and collection
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body:
      """
      {
        "name": "test-evaluation-job-oob-collection",
        "collection": {
          "id": "toxicity-and-ethical-principles"
        },
        "model": {
          "url": "{{env:MODEL_URL|http://test.com}}",
          "name": "{{env:MODEL_NAME|test}}"
        },
        "benchmarks": [
          {
            "id": "arc_easy",
            "provider_id": "lm_evaluation_harness",
            "parameters": {
              "num_examples": 10,
              "num_fewshot": 3,
              "limit": 5,
              "tokenizer": "google/flan-t5-small"
            }
          }
        ]
      }
      """
    And the response code should be 400
    And the response should contain the value "request_validation_failed" at path "$.message_code"
    And the response should contain the value "'benchmarks' failed on the 'benchmarks or collection' tag'." at path "$.message"

  @negative
  Scenario: Verify Evaluation Jobs require either benchmarks or collection
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body:
      """
      {
        "name": "test-evaluation-job-oob-collection",
        "model": {
          "url": "{{env:MODEL_URL|http://test.com}}",
          "name": "{{env:MODEL_NAME|test}}"
        }
      }
      """
    Then the response code should be 400
    And the response should contain the value "request_validation_failed" at path "$.message_code"
    And the response should contain the value "benchmarks' failed on the 'minimum one benchmark' tag" at path "$.message"

  @negative
  Scenario: Cannot create job with empty OOB collection id
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body:
      """
      {
        "name": "test-empty-collection-id",
        "model": {
          "url": "{{env:MODEL_URL|http://test.com}}",
          "name": "{{env:MODEL_NAME|test}}"
        },
        "collection": {
          "id": ""
        }
      }
      """
    Then the response code should be 400
    And the response should contain the value "request_validation_failed" at path "$.message_code"

  @negative
  Scenario: Cannot create job with null OOB collection id
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body:
      """
      {
        "name": "test-null-collection-id",
        "model": {
          "url": "{{env:MODEL_URL|http://test.com}}",
          "name": "{{env:MODEL_NAME|test}}"
        },
        "collection": {
          "id": null
        }
      }
      """
    Then the response code should be 400
    And the response should contain the value "request_validation_failed" at path "$.message_code"

  @negative
  Scenario: Cannot create job with typo in OOB collection id
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body:
      """
      {
        "name": "test-typo-collection-id",
        "model": {
          "url": "{{env:MODEL_URL|http://test.com}}",
          "name": "{{env:MODEL_NAME|test}}"
        },
        "collection": {
          "id": "toxicity-and-ethical-principle"
        }
      }
      """
    Then the response code should be 404
    And the response should contain the value "resource_not_found" at path "$.message_code"

  @negative
  Scenario: Cannot create job with wrong case in OOB collection id
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body:
      """
      {
        "name": "test-wrong-case-collection-id",
        "model": {
          "url": "{{env:MODEL_URL|http://test.com}}",
          "name": "{{env:MODEL_NAME|test}}"
        },
        "collection": {
          "id": "Toxicity-And-Ethical-Principles"
        }
      }
      """
    Then the response code should be 404
    And the response should contain the value "resource_not_found" at path "$.message_code"

  @negative
  Scenario: Cannot create job with whitespace in OOB collection id
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body:
      """
      {
        "name": "test-whitespace-collection-id",
        "model": {
          "url": "{{env:MODEL_URL|http://test.com}}",
          "name": "{{env:MODEL_NAME|test}}"
        },
        "collection": {
          "id": "  toxicity-and-ethical-principles  "
        }
      }
      """
    Then the response code should be 404
    And the response should contain the value "resource_not_found" at path "$.message_code"

  @negative
  # Expected to fail on 3.5 EA1 builds - was fixed in 3.5 EA2 (RHOAIENG-62529)
  Scenario: Cannot override OOB collection benchmark with invalid provider_id
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body:
      """
      {
        "name": "test-invalid-provider-override",
        "model": {
          "url": "{{env:MODEL_URL|http://test.com}}",
          "name": "{{env:MODEL_NAME|test}}"
        },
        "collection": {
          "id": "toxicity-and-ethical-principles",
          "benchmarks": [
            {
              "id": "toxigen",
              "provider_id": "invalid_provider",
              "parameters": {
                "limit": 5
              }
            }
          ]
        }
      }
      """
    Then the response code should be 400
    And the response should contain the value "resource_does_not_exist" at path "$.message_code"

  @negative
  Scenario: Cannot override OOB collection benchmark with empty benchmark_id
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body:
      """
      {
        "name": "test-empty-benchmark-override",
        "model": {
          "url": "{{env:MODEL_URL|http://test.com}}",
          "name": "{{env:MODEL_NAME|test}}"
        },
        "collection": {
          "id": "toxicity-and-ethical-principles",
          "benchmarks": [
            {
              "id": "",
              "provider_id": "lm_evaluation_harness",
              "parameters": {
                "limit": 5
              }
            }
          ]
        }
      }
      """
    Then the response code should be 400
    And the response should contain the value "request_validation_failed" at path "$.message_code"

  @negative
  Scenario: Cannot override OOB collection benchmark with null benchmark_id
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body:
      """
      {
        "name": "test-null-benchmark-override",
        "model": {
          "url": "{{env:MODEL_URL|http://test.com}}",
          "name": "{{env:MODEL_NAME|test}}"
        },
        "collection": {
          "id": "toxicity-and-ethical-principles",
          "benchmarks": [
            {
              "id": null,
              "provider_id": "lm_evaluation_harness",
              "parameters": {
                "limit": 5
              }
            }
          ]
        }
      }
      """
    Then the response code should be 400
    And the response should contain the value "request_validation_failed" at path "$.message_code"

  @negative
  # Expected to fail on 3.5 EA1 builds - was fixed in 3.5 EA2 (RHOAIENG-62531)
  Scenario: Cannot override OOB collection benchmark with incorrect benchmark_id
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body:
      """
      {
        "name": "test-typo-benchmark-override",
        "model": {
          "url": "{{env:MODEL_URL|http://test.com}}",
          "name": "{{env:MODEL_NAME|test}}"
        },
        "collection": {
          "id": "toxicity-and-ethical-principles",
          "benchmarks": [
            {
              "id": "toxigen_typo",
              "provider_id": "lm_evaluation_harness",
              "parameters": {
                "limit": 5
              }
            }
          ]
        }
      }
      """
    Then the response code should be 400
    And the response should contain the value "resource_does_not_exist" at path "$.message_code"

  @kueue
  Scenario: Create evaluation job with Kueue queue
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job_kueue.json"
    Then the response code should be 202
    And the response should contain the value "kueue" at path "$.queue.kind"
    And the response should contain the value "{{env:QUEUE_NAME|user-queue}}" at path "$.queue.name"
    And I wait for the evaluation job status to be "completed"
    When I send a GET request to "/api/v1/evaluations/jobs/{id}"
    Then the response code should be 200
    And the response should contain the value "completed" at path "$.status.state"
    And the response should contain the value "kueue" at path "$.queue.kind"
    And the response should contain the value "{{env:QUEUE_NAME|user-queue}}" at path "$.queue.name"

  @kueue
  Scenario: Create evaluation job with queue name only
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job_kueue_name_only.json"
    Then the response code should be 202
    And the response should contain the value "kueue" at path "$.queue.kind"
    And the response should contain the value "{{env:QUEUE_NAME|user-queue}}" at path "$.queue.name"
    And I wait for the evaluation job status to be "completed"
    When I send a GET request to "/api/v1/evaluations/jobs/{id}"
    Then the response code should be 200
    And the response should contain the value "completed" at path "$.status.state"
    And the response should contain the value "kueue" at path "$.queue.kind"
    And the response should contain the value "{{env:QUEUE_NAME|user-queue}}" at path "$.queue.name"

  @kueue
  @negative
  Scenario: Cannot create job with invalid queue kind
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body:
      """
      {
        "model": {
          "url": "{{env:MODEL_URL|http://test.com}}",
          "name": "{{env:MODEL_NAME|test}}"
        },
        "benchmarks": [
          {
            "id": "arc_easy",
            "provider_id": "lm_evaluation_harness",
            "parameters": {
              "num_examples": 10,
              "num_fewshot": 3,
              "limit": 5,
              "tokenizer": "google/flan-t5-small"
            }
          }
        ],
        "name": "test-invalid-queue-kind",
        "queue": {
          "kind": "invalid-kind",
          "name": "{{env:QUEUE_NAME|user-queue}}"
        }
      }
      """
    Then the response code should be 400
    And the response should contain the value "request_validation_failed" at path "$.message_code"

  @kueue
  @negative
  Scenario: Cannot create job with queue missing name
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body:
      """
      {
        "model": {
          "url": "{{env:MODEL_URL|http://test.com}}",
          "name": "{{env:MODEL_NAME|test}}"
        },
        "benchmarks": [
          {
            "id": "arc_easy",
            "provider_id": "lm_evaluation_harness",
            "parameters": {
              "num_examples": 10,
              "num_fewshot": 3,
              "limit": 5,
              "tokenizer": "google/flan-t5-small"
            }
          }
        ],
        "name": "missing-queue-name",
        "queue": {
          "kind": "kueue"
        }
      }
      """
    Then the response code should be 400
    And the response should contain the value "request_validation_failed" at path "$.message_code"

  @kueue
  # This scenario requires HuggingFace authentication for all 3 benchmarks to run
  Scenario: Create evaluation job with queue and collection
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job_kueue_oob_toxicity.jsonnet"
    Then the response code should be 202
    And the response should contain the value "kueue" at path "$.queue.kind"
    And the response should contain the value "{{env:QUEUE_NAME|user-queue}}" at path "$.queue.name"
    And the response should contain the value "toxicity-and-ethical-principles" at path "$.collection.id"
    And I wait for the evaluation job status to be "completed"
    When I send a GET request to "/api/v1/evaluations/jobs/{id}"
    Then the response code should be 200
    And the response should contain the value "completed" at path "$.status.state"
    And the response should contain the value "toxicity-and-ethical-principles" at path "$.collection.id"
    And the response should contain the value "kueue" at path "$.queue.kind"
    And the response should contain the value "{{env:QUEUE_NAME|user-queue}}" at path "$.queue.name"

  @kueue
  @mlflow
  Scenario: Create evaluation job with queue and MLflow experiment
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body:
      """
      {
        "model": {
          "url": "{{env:MODEL_URL|http://test.com}}",
          "name": "{{env:MODEL_NAME|test}}"
        },
        "benchmarks": [
          {
            "id": "arc_easy",
            "provider_id": "lm_evaluation_harness",
            "parameters": {
              "num_examples": 10,
              "num_fewshot": 3,
              "limit": 5,
              "tokenizer": "google/flan-t5-small"
            }
          }
        ],
        "experiment": {
          "name": "{{mlflow:my-test-experiment}}",
          "tags": [
            {
              "key": "environment",
              "value": "test"
            }
          ]
        },
        "name": "test-evaluation-job-with-kueue-and-mlflow",
        "queue": {
          "kind": "kueue",
          "name": "{{env:QUEUE_NAME|user-queue}}"
        },
        "tags": [
          "environment"
        ]
      }
      """
    Then the response code should be 202
    And the response should contain the value "kueue" at path "$.queue.kind"
    And the response should contain the value "{{env:QUEUE_NAME|user-queue}}" at path "$.queue.name"
    And the "resource.mlflow_experiment_id" field in the response should be saved as "value:exp_id"
    And the response should contain the value "my-test-experiment" at path "$.experiment.name"
    And the response should contain the value "mlflow" at path "$.results.mlflow_experiment_url"
    And the response should contain the value "environment" at path "$.experiment.tags[0].key"
    And the response should contain the value "test" at path "$.experiment.tags[0].value"
    And I wait for the evaluation job status to be "completed"
    When I send a GET request to "/api/v1/evaluations/jobs/{id}"
    Then the response code should be 200
    And the response should contain the value "kueue" at path "$.queue.kind"
    And the response should contain the value "{{env:QUEUE_NAME|user-queue}}" at path "$.queue.name"
    And the response should contain the value "environment" at path "$.experiment.tags[0].key"
    And the response should contain the value "test" at path "$.experiment.tags[0].value"
    And the response should contain the value "{{value:exp_id}}" at path "$.resource.mlflow_experiment_id"
    And the response should contain the value "mlflow" at path "$.results.mlflow_experiment_url"
    And the response should contain the value "my-test-experiment" at path "$.experiment.name"

  @kueue
  @negative
  Scenario: Create evaluation job with queue name containing whitespace fails
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job_kueue_whitespace.json"
    Then the response code should be 400
    And the response should contain the value "request_validation_failed" at path "$.message_code"

  @kueue
  Scenario: Multiple jobs can use the same queue
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job_kueue_shared_job1.json"
    Then the response code should be 202
    And the response should contain the value "{{env:QUEUE_NAME|user-queue}}" at path "$.queue.name"
    And the "resource.id" field in the response should be saved as "value:job1_id"
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job_kueue_shared_job2.json"
    Then the response code should be 202
    And the response should contain the value "{{env:QUEUE_NAME|user-queue}}" at path "$.queue.name"
    And the "resource.id" field in the response should be saved as "value:job2_id"
    When I send a GET request to "/api/v1/evaluations/jobs/{{value:job1_id}}"
    Then the response code should be 200
    And I wait for the evaluation job status to be "completed"
    And the response should contain the value "completed" at path "$.status.state"
    And the response should contain the value "kueue" at path "$.queue.kind"
    And the response should contain the value "{{env:QUEUE_NAME|user-queue}}" at path "$.queue.name"
    When I send a GET request to "/api/v1/evaluations/jobs/{{value:job2_id}}"
    Then the response code should be 200
    And I wait for the evaluation job status to be "completed"
    And the response should contain the value "completed" at path "$.status.state"
    And the response should contain the value "kueue" at path "$.queue.kind"
    And the response should contain the value "{{env:QUEUE_NAME|user-queue}}" at path "$.queue.name"

  @kueue
  @negative
  Scenario: Queue name with special characters is rejected
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body:
      """
      {
        "model": {
          "url": "{{env:MODEL_URL|http://test.com}}",
          "name": "{{env:MODEL_NAME|test}}"
        },
        "benchmarks": [
          {
            "id": "arc_easy",
            "provider_id": "lm_evaluation_harness",
            "parameters": {
              "num_examples": 10,
              "num_fewshot": 3,
              "limit": 5,
              "tokenizer": "google/flan-t5-small"
            }
          }
        ],
        "name": "test-job-special-chars-queue",
        "queue": {
          "kind": "kueue",
          "name": "user-queue!@#$%"
        }
      }
      """
    Then the response code should be 400
    And the response should contain the value "request_validation_failed" at path "$.message_code"

  @kueue
  @negative
  Scenario: Queue with null name is rejected
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body:
      """
      {
        "model": {
          "url": "{{env:MODEL_URL|http://test.com}}",
          "name": "{{env:MODEL_NAME|test}}"
        },
        "benchmarks": [
          {
            "id": "arc_easy",
            "provider_id": "lm_evaluation_harness",
            "parameters": {
              "num_examples": 10,
              "num_fewshot": 3,
              "limit": 5,
              "tokenizer": "google/flan-t5-small"
            }
          }
        ],
        "name": "test-job-null-queue",
        "queue": {
          "kind": "kueue",
          "name": null
        }
      }
      """
    Then the response code should be 400
    And the response should contain the value "request_validation_failed" at path "$.message_code"

  @hardware_profile
  Scenario: Create evaluation job with hardware profile persists reference in API response
    Given the service is running
    And the test hardware profile is configured
    When I send a POST request to "/api/v1/evaluations/jobs" with body:
      """
      {
        "name": "test-evaluation-job-hardware-profile",
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
              "num_examples": 3,
              "tokenizer": "{{env:FVT_BENCHMARK_TOKENIZER|google/flan-t5-small}}"
            },
            "hardware_config": {
              "hardware_profile_ref": {
                "name": "{{env:TEST_HARDWARE_PROFILE}}"
              }
            }
          }
        ]
      }
      """
    Then the response code should be 202
    And the response should contain the value "evaluation_job_created" at path "$.status.message.message_code"
    And the response should contain the value "{{env:TEST_HARDWARE_PROFILE}}" at path "$.benchmarks[0].hardware_config.hardware_profile_ref.name"
    And I wait for the Kubernetes evaluation Job to be created
    And the Job adapter container should have CPU request "{{env:TEST_HARDWARE_PROFILE_CPU_REQUEST}}"
    And the Job adapter container should have memory request "{{env:TEST_HARDWARE_PROFILE_MEMORY_REQUEST}}"
    And the Job adapter container should have CPU limit "{{env:TEST_HARDWARE_PROFILE_CPU_LIMIT}}"
    And the Job adapter container should have memory limit "{{env:TEST_HARDWARE_PROFILE_MEMORY_LIMIT}}"
    When I send a GET request to "/api/v1/evaluations/jobs/{id}"
    Then the response code should be 200
    And the response should contain the value "{{env:TEST_HARDWARE_PROFILE}}" at path "$.benchmarks[0].hardware_config.hardware_profile_ref.name"
    And the response should not contain the value "" at path "$.benchmarks[0].hardware_config.hardware_profile_ref.namespace"
    When I send a DELETE request to "/api/v1/evaluations/jobs/{id}?hard_delete=true"
    Then the response code should be 204

  @hardware_profile
  Scenario: Create evaluation job with explicit hardware profile namespace in API response
    Given the service is running
    And the test hardware profile is configured
    When I send a POST request to "/api/v1/evaluations/jobs" with body:
      """
      {
        "name": "test-evaluation-job-hardware-profile-explicit-ns",
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
              "num_examples": 3,
              "tokenizer": "{{env:FVT_BENCHMARK_TOKENIZER|google/flan-t5-small}}"
            },
            "hardware_config": {
              "hardware_profile_ref": {
                "name": "{{env:TEST_HARDWARE_PROFILE}}",
                "namespace": "{{env:X_TENANT|test-tenant}}"
              }
            }
          }
        ]
      }
      """
    Then the response code should be 202
    And the response should contain the value "{{env:TEST_HARDWARE_PROFILE}}" at path "$.benchmarks[0].hardware_config.hardware_profile_ref.name"
    And the response should contain the value "{{env:X_TENANT|test-tenant}}" at path "$.benchmarks[0].hardware_config.hardware_profile_ref.namespace"
    And I wait for the Kubernetes evaluation Job to be created
    And the Job adapter container should have CPU request "{{env:TEST_HARDWARE_PROFILE_CPU_REQUEST}}"
    And the Job adapter container should have memory request "{{env:TEST_HARDWARE_PROFILE_MEMORY_REQUEST}}"
    When I send a DELETE request to "/api/v1/evaluations/jobs/{id}?hard_delete=true"
    Then the response code should be 204

  @kueue
  Scenario: Create evaluation job with Kueue queue, tags and pass criteria
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job_kueue_tags_criteria.json"
    Then the response code should be 202
    And the response should contain the value "kueue" at path "$.queue.kind"
    And the response should contain the value "{{env:QUEUE_NAME|user-queue}}" at path "$.queue.name"
    And the response should contain the value "integration-test" at path "$.tags[0]"
    And the response should contain the value "kueue-enabled" at path "$.tags[1]"
    And the response should equal the value "0.8" at path "$.pass_criteria.threshold"
    And I wait for the evaluation job status to be "completed"
    When I send a GET request to "/api/v1/evaluations/jobs/{id}"
    Then the response code should be 200
    And the response should contain the value "completed" at path "$.status.state"
    And the response should contain the value "kueue" at path "$.queue.kind"
    And the response should contain the value "{{env:QUEUE_NAME|user-queue}}" at path "$.queue.name"
    And the response should contain the value "integration-test" at path "$.tags[0]"
    And the response should contain the value "kueue-enabled" at path "$.tags[1]"
    And the response should equal the value "0.8" at path "$.pass_criteria.threshold"
