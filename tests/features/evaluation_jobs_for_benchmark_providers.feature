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

  # https://redhat.atlassian.net/browse/RHOAIENG-99472
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
    And the array at path "results.benchmarks" in the response should have length 3
    And all benchmarks in the response should have status "completed"
    And all benchmarks in the response should have metrics matching the provider config
    And all benchmarks in the response should have a test block with pass criteria
    When I send a DELETE request to "/api/v1/evaluations/jobs/{id}?hard_delete=true"
    Then the response code should be 204

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
    And the array at path "results.benchmarks" in the response should have length 1
    And all benchmarks in the response should have status "completed"
    And all benchmarks in the response should have metrics matching the provider config
    And all benchmarks in the response should have additional_info matching the expected fields
    And all benchmarks in the response should have a test block with pass criteria
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
    And the array at path "results.benchmarks" in the response should have length 1
    And all benchmarks in the response should have status "completed"
    And all benchmarks in the response should have metrics matching the provider config
    And all benchmarks in the response should have additional_info matching the expected fields
    And all benchmarks in the response should have a test block with pass criteria
    When I send a DELETE request to "/api/v1/evaluations/jobs/{id}?hard_delete=true"
    Then the response code should be 204

  # vLLM chat completions endpoint (/v1/chat/completions) supports greedy_until
  # https://github.com/huggingface/lighteval/issues/1130
  # https://redhat.atlassian.net/browse/RHOAIENG-99618
  # https://redhat.atlassian.net/browse/RHOAIENG-99621
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
    And the array at path "results.benchmarks" in the response should have length 6
    And all benchmarks in the response should have status "completed"
    And all benchmarks in the response should have metrics matching the provider config
    And all benchmarks in the response should have additional_info matching the expected fields
    And all benchmarks in the response should have a test block with pass criteria
    When I send a DELETE request to "/api/v1/evaluations/jobs/{id}?hard_delete=true"
    Then the response code should be 204

  # Additional generative reasoning tasks for the same endpoint backend.
  # LightEval's LiteLLM endpoint model does not implement loglikelihood.
  Scenario: Verifying results returned for Evaluation job - lighteval - greedy_until - group 2
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job_lighteval_group2.json"
    Then the response code should be 202
    And the response should contain the value "pending" at path "$.status.state"
    And the response should contain the value "evaluation_job_created" at path "$.status.message.message_code"
    And I wait for the evaluation job status to be "completed"
    When I send a GET request to "/api/v1/evaluations/jobs/{id}"
    Then the response code should be 200
    And the response should contain the value "completed" at path "$.status.state"
    And the response should contain the value "{{env:MODEL_NAME|test}}" at path "$.model.name"
    And the response should contain the value "{{env:MODEL_URL|http://test.com}}" at path "$.model.url"
    And the response should contain the value "test-evaluation-job-for-lighteval-benchmark-greedy_until-group2" at path "$.name"
    And the response should contain "results"
    And the array at path "results.benchmarks" in the response should have length 5
    And all benchmarks in the response should have status "completed"
    And all benchmarks in the response should have metrics matching the provider config
    And all benchmarks in the response should have additional_info matching the expected fields
    And all benchmarks in the response should have a test block with pass criteria
    When I send a DELETE request to "/api/v1/evaluations/jobs/{id}?hard_delete=true"
    Then the response code should be 204

  # Following lm_evaluation_harness benchmark needs a valid HF token
  # Running all lm_evaluation_harness benchmarks in a single job isn't viable — the model and HuggingFace both choke.
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
    And all benchmarks in the response should have additional_info matching the expected fields
    And all benchmarks in the response should have a test block with pass criteria
    When I send a DELETE request to "/api/v1/evaluations/jobs/{id}?hard_delete=true"
    Then the response code should be 204

  # https://redhat.atlassian.net/browse/RHOAIENG-85389
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
    And the array at path "results.benchmarks" in the response should have length 23
    And all benchmarks in the response should have status "completed"
    And all benchmarks in the response should have metrics matching the provider config
    And all benchmarks in the response should have additional_info matching the expected fields
    And all benchmarks in the response should have a test block with pass criteria
    When I send a DELETE request to "/api/v1/evaluations/jobs/{id}?hard_delete=true"
    Then the response code should be 204

  # https://redhat.atlassian.net/browse/RHOAIENG-85389
  # https://redhat.atlassian.net/browse/RHOAIENG-97629
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
    And the array at path "results.benchmarks" in the response should have length 25
    And all benchmarks in the response should have status "completed"
    And all benchmarks in the response should have metrics matching the provider config
    And all benchmarks in the response should have additional_info matching the expected fields
    And all benchmarks in the response should have a test block with pass criteria
    When I send a DELETE request to "/api/v1/evaluations/jobs/{id}?hard_delete=true"
    Then the response code should be 204

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
    And all benchmarks in the response should have additional_info matching the expected fields
    And all benchmarks in the response should have a test block with pass criteria
    When I send a DELETE request to "/api/v1/evaluations/jobs/{id}?hard_delete=true"
    Then the response code should be 204

  # https://redhat.atlassian.net/browse/RHOAIENG-85389
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
    And the array at path "results.benchmarks" in the response should have length 19
    And all benchmarks in the response should have status "completed"
    And all benchmarks in the response should have metrics matching the provider config
    And all benchmarks in the response should have additional_info matching the expected fields
    And all benchmarks in the response should have a test block with pass criteria
    When I send a DELETE request to "/api/v1/evaluations/jobs/{id}?hard_delete=true"
    Then the response code should be 204

  # https://redhat.atlassian.net/browse/RHOAIENG-85393 - tinyTruthfulQA
  # https://redhat.atlassian.net/browse/RHOAIENG-97651
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
    And the array at path "results.benchmarks" in the response should have length 19
    And all benchmarks in the response should have status "completed"
    And all benchmarks in the response should have metrics matching the provider config
    And all benchmarks in the response should have additional_info matching the expected fields
    And all benchmarks in the response should have a test block with pass criteria
    When I send a DELETE request to "/api/v1/evaluations/jobs/{id}?hard_delete=true"
    Then the response code should be 204

  @connected
  Scenario: Verifying results returned for Evaluation job - ETHICS justice - connected
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job_ethics_justice.json"
    Then the response code should be 202
    And the response should contain the value "pending" at path "$.status.state"
    And I wait for the evaluation job status to be "completed"
    When I send a GET request to "/api/v1/evaluations/jobs/{id}"
    Then the response code should be 200
    And the response should contain the value "completed" at path "$.status.state"
    And the array at path "results.benchmarks" in the response should have length 1
    And the response should contain the value "completed" at path "$.status.benchmarks[0].status"
    And the response should contain the value "ethics_justice" at path "$.results.benchmarks[0].id"
    And the response should contain the value "lm_evaluation_harness" at path "$.results.benchmarks[0].provider_id"
    And the response should contain "results.benchmarks[0].metrics.acc"
    And the response should contain the value "0" at path "$.benchmarks[0].parameters.num_fewshot"
    And the response should contain the value "1" at path "$.benchmarks[0].parameters.num_examples"
    And the response should contain the value "true" at path "$.benchmarks[0].parameters.trust_remote_code"
    And the response should contain the value "completions" at path "$.benchmarks[0].parameters.api_style"
    And the response should contain the value "huggingface" at path "$.benchmarks[0].parameters.tokenizer_backend"
    And the response should contain the value "{{env:FVT_BENCHMARK_TOKENIZER|google/flan-t5-small}}" at path "$.benchmarks[0].parameters.tokenizer"
    And the response should contain the value "false" at path "$.benchmarks[0].parameters.hf_offline"
    When I send a DELETE request to "/api/v1/evaluations/jobs/{id}?hard_delete=true"
    Then the response code should be 204

  @disconnected
  Scenario: Verifying results returned for Evaluation job - ETHICS justice - disconnected
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job_ethics_justice.json"
    Then the response code should be 202
    And the response should contain the value "pending" at path "$.status.state"
    And I wait for the evaluation job status to be "completed"
    When I send a GET request to "/api/v1/evaluations/jobs/{id}"
    Then the response code should be 200
    And the response should contain the value "completed" at path "$.status.state"
    And the array at path "results.benchmarks" in the response should have length 1
    And the response should contain the value "completed" at path "$.status.benchmarks[0].status"
    And the response should contain the value "ethics_justice" at path "$.results.benchmarks[0].id"
    And the response should contain the value "lm_evaluation_harness" at path "$.results.benchmarks[0].provider_id"
    And the response should contain "results.benchmarks[0].metrics.acc"
    And the response should contain the value "0" at path "$.benchmarks[0].parameters.num_fewshot"
    And the response should contain the value "1" at path "$.benchmarks[0].parameters.num_examples"
    And the response should contain the value "true" at path "$.benchmarks[0].parameters.trust_remote_code"
    And the response should contain the value "completions" at path "$.benchmarks[0].parameters.api_style"
    And the response should contain the value "huggingface" at path "$.benchmarks[0].parameters.tokenizer_backend"
    And the response should contain the value "/test_data/tokenizer" at path "$.benchmarks[0].parameters.tokenizer"
    And the response should contain the value "/test_data/hf-cache" at path "$.benchmarks[0].parameters.hf_datasets_cache"
    And the response should contain the value "true" at path "$.benchmarks[0].parameters.hf_offline"
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
    And all benchmarks in the response should have additional_info matching the expected fields
    And all benchmarks in the response should have a test block with pass criteria
    When I send a DELETE request to "/api/v1/evaluations/jobs/{id}?hard_delete=true"
    Then the response code should be 204

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
    And all benchmarks in the response should have a test block with pass criteria
    When I send a DELETE request to "/api/v1/evaluations/jobs/{id}?hard_delete=true"
    Then the response code should be 204

 # https://redhat.atlassian.net/browse/RHOAIENG-97760
  @ignore
  Scenario: Verifying results returned for Evaluation job - MTEB
    Given the service is running
