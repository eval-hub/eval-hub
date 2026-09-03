@cluster
@evaluations
@benchmark_providers
Feature: Evaluation Jobs for Benchmark Providers
  As a data scientist
  I want to run evaluation jobs against all default benchmark providers
  So that I catch upstream breaking changes

  Background:
    Given I set the header "X-Tenant" to "{{env:X_TENANT|test-tenant}}"
    And I set the header "X-User" to "{{env:X_USER|test-user}}"
    And I set the wait deadline to "{{env:WAIT_DEADLINE|30m}}"
    And the model endpoint is reachable
    # This is mandatory for the tests to run successfully
    And the value "{{env:MODEL_AUTH_SECRET_REF}}" is not empty

  # https://redhat.atlassian.net/browse/RHOAIENG-84701 - Garak intents benchmark fails
  Scenario: Verifying results returned for Evaluation job - garak
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job_garak.json"
    Then the response code should be 202
    And the response should contain the value "pending" at path "$.status.state"
    And the response should contain the value "evaluation_job_created" at path "$.status.message.message_code"
    And I wait for the evaluation job status to be "completed"
    When I send a GET request to "/api/v1/evaluations/jobs/{id}"
    Then the response code should be 200
    And the response should contain the value "completed" at path "$.status.state"
    And the response should contain the value "{{env:MODEL_NAME|test}}" at path "$.model.name"
    And the response should contain the value "{{env:MODEL_URL|http://test.com}}" at path "$.model.url"
    And the response should contain the value "test-evaluation-job-for-garak-benchmark" at path "$.name"
    And the response should contain "results"
    And the array at path "results.benchmarks" in the response should have length 9
    And all benchmarks in the response should have status "completed"
    And all benchmarks in the response should have metrics matching the provider config
    When I send a DELETE request to "/api/v1/evaluations/jobs/{id}?hard_delete=true"
    Then the response code should be 204
  @f3
  Scenario: Verifying results returned for Evaluation job - guidellm - group 1
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job_guidellm.json"
    Then the response code should be 202
    And the response should contain the value "pending" at path "$.status.state"
    And the response should contain the value "evaluation_job_created" at path "$.status.message.message_code"
    And I wait for the evaluation job status to be "completed"
    When I send a GET request to "/api/v1/evaluations/jobs/{id}"
    Then the response code should be 200
    And the response should contain the value "completed" at path "$.status.state"
    And the response should contain the value "{{env:MODEL_NAME|test}}" at path "$.model.name"
    And the response should contain the value "{{env:MODEL_URL|http://test.com}}" at path "$.model.url"
    And the response should contain the value "test-evaluation-job-for-guidellm-benchmark" at path "$.name"
    And the response should contain "results"
    And the array at path "results.benchmarks" in the response should have length 4
    And all benchmarks in the response should have status "completed"
    And all benchmarks in the response should have metrics matching the provider config
    When I send a DELETE request to "/api/v1/evaluations/jobs/{id}?hard_delete=true"
    Then the response code should be 204

  Scenario: Verifying results returned for Evaluation job - guidellm - group 2
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job_guidellm_group2.json"
    Then the response code should be 202
    And the response should contain the value "pending" at path "$.status.state"
    And the response should contain the value "evaluation_job_created" at path "$.status.message.message_code"
    And I wait for the evaluation job status to be "completed"
    When I send a GET request to "/api/v1/evaluations/jobs/{id}"
    Then the response code should be 200
    And the response should contain the value "completed" at path "$.status.state"
    And the response should contain the value "{{env:MODEL_NAME|test}}" at path "$.model.name"
    And the response should contain the value "{{env:MODEL_URL|http://test.com}}" at path "$.model.url"
    And the response should contain the value "test-evaluation-job-for-guidellm-benchmark" at path "$.name"
    And the response should contain "results"
    And the array at path "results.benchmarks" in the response should have length 3
    And all benchmarks in the response should have status "completed"
    And all benchmarks in the response should have metrics matching the provider config
    When I send a DELETE request to "/api/v1/evaluations/jobs/{id}?hard_delete=true"
    Then the response code should be 204

  # vLLM chat completions endpoint (/v1/chat/completions) supports greedy_until
  # https://github.com/huggingface/lighteval/issues/1130
  Scenario: Verifying results returned for Evaluation job - lighteval - greedy_until
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job_lighteval_greedy_until.json"
    Then the response code should be 202
    And the response should contain the value "pending" at path "$.status.state"
    And the response should contain the value "evaluation_job_created" at path "$.status.message.message_code"
    And I wait for the evaluation job status to be "completed"
    When I send a GET request to "/api/v1/evaluations/jobs/{id}"
    Then the response code should be 200
    And the response should contain the value "completed" at path "$.status.state"
    And the response should contain the value "{{env:MODEL_NAME|test}}" at path "$.model.name"
    And the response should contain the value "{{env:MODEL_URL|http://test.com}}" at path "$.model.url"
    And the response should contain the value "test-evaluation-job-for-lighteval-benchmark-greedy_until" at path "$.name"
    And the response should contain "results"
    And the array at path "results.benchmarks" in the response should have length 11
    And all benchmarks in the response should have status "completed"
    And all benchmarks in the response should have metrics matching the provider config
    When I send a DELETE request to "/api/v1/evaluations/jobs/{id}?hard_delete=true"
    Then the response code should be 204

  # https://redhat.atlassian.net/browse/RHOAIENG-84704
  # needs an endpoint that support loglikelihood
  Scenario: Verifying results returned for Evaluation job - lighteval - loglikelihood
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job_lighteval_loglikelihood.json"
    Then the response code should be 202
    And the response should contain the value "pending" at path "$.status.state"
    And the response should contain the value "evaluation_job_created" at path "$.status.message.message_code"
    And I wait for the evaluation job status to be "completed"
    When I send a GET request to "/api/v1/evaluations/jobs/{id}"
    Then the response code should be 200
    And the response should contain the value "completed" at path "$.status.state"
    And the response should contain the value "{{env:MODEL_NAME|test}}" at path "$.model.name"
    And the response should contain the value "{{env:MODEL_URL|http://test.com}}" at path "$.model.url"
    And the response should contain the value "test-evaluation-job-for-lighteval-benchmark-loglikelihood" at path "$.name"
    And the response should contain "results"
    And the array at path "results.benchmarks" in the response should have length 17
    And all benchmarks in the response should have status "completed"
    And all benchmarks in the response should have metrics matching the provider config
    When I send a DELETE request to "/api/v1/evaluations/jobs/{id}?hard_delete=true"
    Then the response code should be 204

  # Following lm_evaluation_harness benchmark needs a valid HF token
  # Running all 188 lm_evaluation_harness benchmarks in a single job isn't viable — the model and HuggingFace both choke.
  # Hence smaller groups were made.
  Scenario: Verifying results returned for Evaluation job - lm_evaluation_harness - group 1
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job_lm_evaluation_harness.json"
    Then the response code should be 202
    And the response should contain the value "pending" at path "$.status.state"
    And the response should contain the value "evaluation_job_created" at path "$.status.message.message_code"
    And I wait for the evaluation job status to be "completed"
    When I send a GET request to "/api/v1/evaluations/jobs/{id}"
    Then the response code should be 200
    And the response should contain the value "completed" at path "$.status.state"
    And the response should contain the value "{{env:MODEL_NAME|test}}" at path "$.model.name"
    And the response should contain the value "{{env:MODEL_URL|http://test.com}}" at path "$.model.url"
    And the response should contain the value "test-evaluation-job-for-lm_evaluation_harness-benchmark" at path "$.name"
    And the response should contain "results"
    And the array at path "results.benchmarks" in the response should have length 25
    And all benchmarks in the response should have status "completed"
    And all benchmarks in the response should have metrics matching the provider config
    When I send a DELETE request to "/api/v1/evaluations/jobs/{id}?hard_delete=true"
    Then the response code should be 204

  # https://redhat.atlassian.net/browse/RHOAIENG-85386
  # https://redhat.atlassian.net/browse/RHOAIENG-85389
  # https://redhat.atlassian.net/browse/RHOAIENG-85388
  Scenario: Verifying results returned for Evaluation job - lm_evaluation_harness - group 2
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job_lm_evaluation_harness_2.json"
    Then the response code should be 202
    And the response should contain the value "pending" at path "$.status.state"
    And the response should contain the value "evaluation_job_created" at path "$.status.message.message_code"
    And I wait for the evaluation job status to be "completed"
    When I send a GET request to "/api/v1/evaluations/jobs/{id}"
    Then the response code should be 200
    And the response should contain the value "completed" at path "$.status.state"
    And the response should contain the value "{{env:MODEL_NAME|test}}" at path "$.model.name"
    And the response should contain the value "{{env:MODEL_URL|http://test.com}}" at path "$.model.url"
    And the response should contain the value "test-evaluation-job-for-lm_evaluation_harness-benchmark" at path "$.name"
    And the response should contain "results"
    And the array at path "results.benchmarks" in the response should have length 30
    And all benchmarks in the response should have status "completed"
    And all benchmarks in the response should have metrics matching the provider config
    When I send a DELETE request to "/api/v1/evaluations/jobs/{id}?hard_delete=true"
    Then the response code should be 204

  # https://redhat.atlassian.net/browse/RHOAIENG-85389
  # https://redhat.atlassian.net/browse/RHOAIENG-85386
  Scenario: Verifying results returned for Evaluation job - lm_evaluation_harness - group 3
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job_lm_evaluation_harness_3.json"
    Then the response code should be 202
    And the response should contain the value "pending" at path "$.status.state"
    And the response should contain the value "evaluation_job_created" at path "$.status.message.message_code"
    And I wait for the evaluation job status to be "completed"
    When I send a GET request to "/api/v1/evaluations/jobs/{id}"
    Then the response code should be 200
    And the response should contain the value "completed" at path "$.status.state"
    And the response should contain the value "{{env:MODEL_NAME|test}}" at path "$.model.name"
    And the response should contain the value "{{env:MODEL_URL|http://test.com}}" at path "$.model.url"
    And the response should contain the value "test-evaluation-job-for-lm_evaluation_harness-benchmark" at path "$.name"
    And the response should contain "results"
    And the array at path "results.benchmarks" in the response should have length 30
    And all benchmarks in the response should have status "completed"
    And all benchmarks in the response should have metrics matching the provider config
    When I send a DELETE request to "/api/v1/evaluations/jobs/{id}?hard_delete=true"
    Then the response code should be 204

  # https://redhat.atlassian.net/browse/RHOAIENG-90798 - bbh benchmark
  Scenario: Verifying results returned for Evaluation job - lm_evaluation_harness - group 4
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job_lm_evaluation_harness_4.json"
    Then the response code should be 202
    And the response should contain the value "pending" at path "$.status.state"
    And the response should contain the value "evaluation_job_created" at path "$.status.message.message_code"
    And I wait for the evaluation job status to be "completed"
    When I send a GET request to "/api/v1/evaluations/jobs/{id}"
    Then the response code should be 200
    And the response should contain the value "completed" at path "$.status.state"
    And the response should contain the value "{{env:MODEL_NAME|test}}" at path "$.model.name"
    And the response should contain the value "{{env:MODEL_URL|http://test.com}}" at path "$.model.url"
    And the response should contain the value "test-evaluation-job-for-lm_evaluation_harness-benchmark" at path "$.name"
    And the response should contain "results"
    And the array at path "results.benchmarks" in the response should have length 30
    And all benchmarks in the response should have status "completed"
    And all benchmarks in the response should have metrics matching the provider config
    When I send a DELETE request to "/api/v1/evaluations/jobs/{id}?hard_delete=true"
    Then the response code should be 204

  # https://redhat.atlassian.net/browse/RHOAIENG-85389
  # https://redhat.atlassian.net/browse/RHOAIENG-85386
  # https://redhat.atlassian.net/browse/RHOAIENG-85388
  # https://redhat.atlassian.net/browse/RHOAIENG-85393 - careqa_open_perplexity
  Scenario: Verifying results returned for Evaluation job - lm_evaluation_harness - group 5
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job_lm_evaluation_harness_5.json"
    Then the response code should be 202
    And the response should contain the value "pending" at path "$.status.state"
    And the response should contain the value "evaluation_job_created" at path "$.status.message.message_code"
    And I wait for the evaluation job status to be "completed"
    When I send a GET request to "/api/v1/evaluations/jobs/{id}"
    Then the response code should be 200
    And the response should contain the value "completed" at path "$.status.state"
    And the response should contain the value "{{env:MODEL_NAME|test}}" at path "$.model.name"
    And the response should contain the value "{{env:MODEL_URL|http://test.com}}" at path "$.model.url"
    And the response should contain the value "test-evaluation-job-for-lm_evaluation_harness-benchmark" at path "$.name"
    And the response should contain "results"
    And the array at path "results.benchmarks" in the response should have length 30
    And all benchmarks in the response should have status "completed"
    And all benchmarks in the response should have metrics matching the provider config
    When I send a DELETE request to "/api/v1/evaluations/jobs/{id}?hard_delete=true"
    Then the response code should be 204

  # https://redhat.atlassian.net/browse/RHOAIENG-85386
  # https://redhat.atlassian.net/browse/RHOAIENG-85388
  # https://redhat.atlassian.net/browse/RHOAIENG-85393 - tinyTruthfulQA
  # https://redhat.atlassian.net/browse/RHOAIENG-85410 - humaneval, mbpp
  Scenario: Verifying results returned for Evaluation job - lm_evaluation_harness - group 6
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job_lm_evaluation_harness_6.json"
    Then the response code should be 202
    And the response should contain the value "pending" at path "$.status.state"
    And the response should contain the value "evaluation_job_created" at path "$.status.message.message_code"
    And I wait for the evaluation job status to be "completed"
    When I send a GET request to "/api/v1/evaluations/jobs/{id}"
    Then the response code should be 200
    And the response should contain the value "completed" at path "$.status.state"
    And the response should contain the value "{{env:MODEL_NAME|test}}" at path "$.model.name"
    And the response should contain the value "{{env:MODEL_URL|http://test.com}}" at path "$.model.url"
    And the response should contain the value "test-evaluation-job-for-lm_evaluation_harness-benchmark" at path "$.name"
    And the response should contain "results"
    And the array at path "results.benchmarks" in the response should have length 38
    And all benchmarks in the response should have status "completed"
    And all benchmarks in the response should have metrics matching the provider config
    When I send a DELETE request to "/api/v1/evaluations/jobs/{id}?hard_delete=true"
    Then the response code should be 204

  Scenario: Verifying results returned for Evaluation job - lm_evaluation_harness - group 7
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job_lm_evaluation_harness_7.json"
    Then the response code should be 202
    And the response should contain the value "pending" at path "$.status.state"
    And the response should contain the value "evaluation_job_created" at path "$.status.message.message_code"
    And I wait for the evaluation job status to be "completed"
    When I send a GET request to "/api/v1/evaluations/jobs/{id}"
    Then the response code should be 200
    And the response should contain the value "completed" at path "$.status.state"
    And the response should contain the value "{{env:MODEL_NAME|test}}" at path "$.model.name"
    And the response should contain the value "{{env:MODEL_URL|http://test.com}}" at path "$.model.url"
    And the response should contain the value "test-evaluation-job-for-lm_evaluation_harness-benchmark" at path "$.name"
    And the response should contain "results"
    And the array at path "results.benchmarks" in the response should have length 5
    And all benchmarks in the response should have status "completed"
    And all benchmarks in the response should have metrics matching the provider config
    When I send a DELETE request to "/api/v1/evaluations/jobs/{id}?hard_delete=true"
    Then the response code should be 204

  # https://redhat.atlassian.net/browse/RHOAIENG-89382 - Fails for meta-llama/Llama-3.1-8B-Instruct
  # https://redhat.atlassian.net/browse/RHOAIENG-89395
  Scenario: Verifying results returned for Evaluation job - ragas
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job_ragas.json"
    Then the response code should be 202
    And the response should contain the value "pending" at path "$.status.state"
    And the response should contain the value "evaluation_job_created" at path "$.status.message.message_code"
    And I wait for the evaluation job status to be "completed"
    When I send a GET request to "/api/v1/evaluations/jobs/{id}"
    Then the response code should be 200
    And the response should contain the value "completed" at path "$.status.state"
    And the response should contain the value "{{env:MODEL_NAME|test}}" at path "$.model.name"
    And the response should contain the value "{{env:MODEL_URL|http://test.com}}" at path "$.model.url"
    And the response should contain the value "test-evaluation-job-for-ragas-benchmark" at path "$.name"
    And the response should contain "results"
    And the array at path "results.benchmarks" in the response should have length 2
    And all benchmarks in the response should have status "completed"
    And all benchmarks in the response should have metrics matching the provider config
    When I send a DELETE request to "/api/v1/evaluations/jobs/{id}?hard_delete=true"
    Then the response code should be 204

  # MTEB not implemented due to https://redhat.atlassian.net/browse/RHOAIENG-85265
  @ignore
  Scenario: Verifying results returned for Evaluation job - MTEB
    Given the service is running
