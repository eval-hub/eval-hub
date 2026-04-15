@evaluations
@cluster
Feature: Evaluation Jobs
  As a data scientist
  I want to create evaluation jobs against a running cluster
  So that I evaluate models

  Background:
    Given I set the header "X-Tenant" to "{{env:X_TENANT|test-tenant}}"

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
    And the response should contain the value "google/flan-t5-small" at path "$.benchmarks[0].parameters.tokenizer"
    And the response should not contain the value "collection" at path "$.collection"
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
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job_with_collection.json"
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
    When I send a POST request to "/api/v1/evaluations/collections" with body:
      """
      {
        "name": "test-multi-benchmarks-collection",
        "description": "Collection of multiple benchmarks for FVT",
        "category": "test",
        "benchmarks": [
          {
            "id": "arc_easy",
            "provider_id": "lm_evaluation_harness",
            "parameters": {
              "tokenizer": "google/flan-t5-small",
              "limit": 5,
              "num_examples": 10
            }
          },
          {
            "id": "arc_easy",
            "provider_id": "lm_evaluation_harness",
            "parameters": {
              "num_examples": 15,
              "num_fewshot": 3,
              "limit": 5,
              "tokenizer": "google/flan-t5-small"
            }
          }
        ]
      }
      """
    Then the response code should be 201
    And the "resource.id" field in the response should be saved as "value:collection_id"
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job_with_collection.json"
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
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job_with_collection.json"
    Then the response code should be 202
    And the "resource.id" field in the response should be saved as "value:job1_id"
    And I set the header "X-User" to "test-user-2"
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job_with_collection.json"
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
    When I send a POST request to "/api/v1/evaluations/jobs" with body:
      """
      {
        "name": "automation_shared_experiment_with_collections_job_1",
        "model": {
          "url": "{{env:MODEL_URL|http://test.com}}",
          "name": "{{env:MODEL_NAME|test}}"
        },
        "collection": {
          "id": "{{value:collection_id}}"
        },
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
        "name": "automation_shared_experiment_with_collections_job_2",
        "model": {
          "url": "{{env:MODEL_URL|http://test.com}}",
          "name": "{{env:MODEL_NAME|test}}"
        },
        "collection": {
          "id": "{{value:collection_id}}"
        },
        "experiment": {
          "name": "automation_shared_experiment"
        }
      }
      """
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
    When I send a POST request to "/api/v1/evaluations/collections" with body:
      """
      {
        "name": "job-collection-override",
        "description": "Override parameter",
        "category": "test",
        "benchmarks": [
          {
            "id": "arc_easy",
            "provider_id": "lm_evaluation_harness",
            "parameters": {
              "num_examples": 3,
              "tokenizer": "google/flan-t5-small"
            }
          }
        ]
      }
      """
    Then the response code should be 201
    And the "resource.id" field in the response should be saved as "value:collection_id"
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job_with_collection.json"
    Then the response code should be 202
    And the response should contain the value "evaluation_job_created" at path "$.status.message.message_code"
    And the response should contain the value "pending" at path "$.status.state"
    And I wait for the evaluation job status to be "completed"
    When I send a GET request to "/api/v1/evaluations/collections/{{value:collection_id}}"
    Then the response code should be 200
    And the response should contain the value "3" at path "$.benchmarks[0].parameters.num_examples"
    And the response should contain the value "google/flan-t5-small" at path "$.benchmarks[0].parameters.tokenizer"
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
    When I send a POST request to "/api/v1/evaluations/collections" with body:
    """
    {
        "name": "test-benchmarks-collection-threshold-zero",
        "category": "test",
        "description": "Collection of benchmarks for FVT",
        "pass_criteria": {
            "threshold": 0
        },
        "benchmarks": [
            {
                "id": "arc_easy",
                "provider_id": "lm_evaluation_harness",
                "primary_score": {
                    "metric": "acc_norm",
                    "lower_is_better": false
                },
                "pass_criteria": {
                    "threshold": 0.5
                },
                "parameters": {
                    "limit": 10,
                    "num_fewshot": 0,
                    "tokenizer": "google/flan-t5-small"
                }
            },
            {
                "id": "arc_easy",
                "provider_id": "lm_evaluation_harness",
                "primary_score": {
                    "metric": "acc_norm",
                    "lower_is_better": false
                },
                "pass_criteria": {
                    "threshold": 0.5
                },
                "parameters": {
                    "limit": 10,
                    "num_fewshot": 0,
                    "tokenizer": "google/flan-t5-small"
                }
            }
        ]
    }
    """
    Then the response code should be 201
    And the "resource.id" field in the response should be saved as "value:collection_id"
    And the response should contain the value "test-benchmarks-collection-threshold-zero" at path "$.name"
    And the response should contain the value "test" at path "$.category"
    And the response should contain the value "Collection of benchmarks for FVT" at path "$.description"
    And the response should contain the value "0" at path "$.pass_criteria.threshold"
    And the array at path "$.benchmarks" in the response should have length 2
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job_with_collection.json"
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
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job_with_collection.json"
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
