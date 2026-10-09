@evaluations
@post_processing
Feature: Post-Processing Endpoint
  As a data scientist
  I want to submit post-processing computations against evaluation results
  So that I can analyze completed evaluation jobs

  Background:
    Given I set the header "X-Tenant" to "{{env:X_TENANT|test-tenant}}"
    And I set the header "X-User" to "{{env:X_USER|test-user}}"

  @local
  Scenario: Create post-processing computation from a completed evaluation job
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job.json"
    Then the response code should be 202
    And the "resource.id" field in the response should be saved as "value:source_job_id"
    When I send a POST request to "/api/v1/evaluations/jobs/{id}/events" with body "file:/evaluation_job_status_event_running.json"
    Then the response code should be 204
    When I send a POST request to "/api/v1/evaluations/jobs/{id}/events" with body:
    """
    {
      "benchmark_status_event": {
        "id": "arc_easy",
        "provider_id": "lm_evaluation_harness",
        "status": "completed",
        "metrics": {"accuracy": 0.7},
        "metrics_schema": [{"name": "accuracy", "type": "numeric"}]
      }
    }
    """
    Then the response code should be 204
    When I send a GET request to "/api/v1/evaluations/jobs/{id}"
    Then the response code should be 200
    And the response should contain the value "completed" at path "$.status.state"
    And the response should contain the value "0.7" at path "$.results.benchmarks[0].metrics.accuracy"
    When I send a POST request to "/api/v1/evaluations/post-processing" with body:
    """
    {
      "name": "confidence-interval-fvt",
      "operations": {
        "confidence_interval": {
          "results_data_ref": {
            "eval_job": {
              "id": "{{value:source_job_id}}"
            }
          },
          "calibration_data_ref": [
            {
              "pvc": {"claim_name": "calibration-data"},
              "data_config": {
                "format": "jsonl",
                "columns": {
                  "label": "label",
                  "prediction": "prediction"
                }
              }
            }
          ],
          "significance_level": 0.05
        }
      }
    }
    """
    Then the response code should be 202
    And the response should contain the value "confidence-interval-fvt" at path "$.name"
    And the response should contain the value "pending" at path "$.status.state"
    And the response should contain the value "{{value:source_job_id}}" at path "$.operations.confidence_interval.results_data_ref.eval_job.id"
    And the "resource.id" field in the response should be saved as "value:post_processing_id"
