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
    # TODO: Add per-benchmark status and metrics_schema checks once resolved: RHOAIENG-84701
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
    And the array at path "results.benchmarks" in the response should have length 4
    And the response should contain the value "completed" at path "$.status.benchmarks[?(@.id == &quot;sweep&quot;)].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[?(@.id == &quot;throughput&quot;)].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[?(@.id == &quot;concurrent&quot;)].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[?(@.id == &quot;constant&quot;)].status"
    And the array at path "$.results.benchmarks[0].metrics_schema" in the response should have length at least 1
    And the array at path "$.results.benchmarks[1].metrics_schema" in the response should have length at least 1
    And the array at path "$.results.benchmarks[2].metrics_schema" in the response should have length at least 1
    And the array at path "$.results.benchmarks[3].metrics_schema" in the response should have length at least 1
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
    And the response should contain the value "completed" at path "$.status.benchmarks[?(@.id == &quot;poisson&quot;)].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[?(@.id == &quot;quick_perf_test&quot;)].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[?(@.id == &quot;comprehensive_perf_test&quot;)].status"
    And the array at path "$.results.benchmarks[0].metrics_schema" in the response should have length at least 1
    And the array at path "$.results.benchmarks[1].metrics_schema" in the response should have length at least 1
    And the array at path "$.results.benchmarks[2].metrics_schema" in the response should have length at least 1
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
    And the response should contain the value "completed" at path "$.status.benchmarks[?(@.id == &quot;knowledge&quot;)].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[?(@.id == &quot;hellaswag&quot;)].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[?(@.id == &quot;openbookqa&quot;)].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[?(@.id == &quot;gsm8k&quot;)].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[?(@.id == &quot;aime24&quot;)].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[?(@.id == &quot;aime25&quot;)].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[?(@.id == &quot;mmlu&quot;)].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[?(@.id == &quot;triviaqa&quot;)].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[?(@.id == &quot;math_500&quot;)].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[?(@.id == &quot;lcb:codegeneration_v6&quot;)].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[?(@.id == &quot;truthfulqa:gen&quot;)].status"
    And the array at path "$.results.benchmarks[0].metrics_schema" in the response should have length at least 1
    And the array at path "$.results.benchmarks[1].metrics_schema" in the response should have length at least 1
    And the array at path "$.results.benchmarks[2].metrics_schema" in the response should have length at least 1
    And the array at path "$.results.benchmarks[3].metrics_schema" in the response should have length at least 1
    And the array at path "$.results.benchmarks[4].metrics_schema" in the response should have length at least 1
    And the array at path "$.results.benchmarks[5].metrics_schema" in the response should have length at least 1
    And the array at path "$.results.benchmarks[6].metrics_schema" in the response should have length at least 1
    And the array at path "$.results.benchmarks[7].metrics_schema" in the response should have length at least 1
    And the array at path "$.results.benchmarks[8].metrics_schema" in the response should have length at least 1
    And the array at path "$.results.benchmarks[9].metrics_schema" in the response should have length at least 1
    And the array at path "$.results.benchmarks[10].metrics_schema" in the response should have length at least 1
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
    # TODO: Add per-benchmark status and metrics_schema checks once resolved: RHOAIENG-84704
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
    And the response should contain the value "completed" at path "$.status.benchmarks[?(@.id == &quot;arc_easy&quot;)].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[?(@.id == &quot;AraDiCE_boolq_lev&quot;)].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[?(@.id == &quot;blimp_anaphor_gender_agreement&quot;)].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[?(@.id == &quot;blimp_animate_subject_trans&quot;)].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[?(@.id == &quot;blimp_coordinate_structure_constraint_complex_left_branch&quot;)].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[?(@.id == &quot;blimp_determiner_noun_agreement_2&quot;)].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[?(@.id == &quot;blimp_determiner_noun_agreement_with_adj_2&quot;)].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[?(@.id == &quot;blimp_determiner_noun_agreement_with_adjective_1&quot;)].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[?(@.id == &quot;blimp_existential_there_object_raising&quot;)].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[?(@.id == &quot;blimp_existential_there_subject_raising&quot;)].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[?(@.id == &quot;blimp_intransitive&quot;)].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[?(@.id == &quot;blimp_irregular_plural_subject_verb_agreement_1&quot;)].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[?(@.id == &quot;blimp_left_branch_island_simple_question&quot;)].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[?(@.id == &quot;blimp_npi_present_2&quot;)].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[?(@.id == &quot;blimp_passive_1&quot;)].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[?(@.id == &quot;AraDiCE_ArabicMMLU_egy&quot;)].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[?(@.id == &quot;AraDiCE_ArabicMMLU_high_humanities_history_lev&quot;)].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[?(@.id == &quot;AraDiCE_ArabicMMLU_high_humanities_philosophy_egy&quot;)].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[?(@.id == &quot;AraDiCE_ArabicMMLU_high_language_arabic-language_lev&quot;)].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[?(@.id == &quot;AraDiCE_ArabicMMLU_lev&quot;)].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[?(@.id == &quot;AraDiCE_ArabicMMLU_middle_humanities_islamic-studies_egy&quot;)].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[?(@.id == &quot;AraDiCE_ArabicMMLU_middle_language_arabic-language_lev&quot;)].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[?(@.id == &quot;AraDiCE_ArabicMMLU_na_humanities_islamic-studies_egy&quot;)].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[?(@.id == &quot;AraDiCE_ArabicMMLU_na_language_arabic-language-general_lev&quot;)].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[?(@.id == &quot;AraDiCE_ArabicMMLU_na_other_driving-test_egy&quot;)].status"
    And the array at path "$.results.benchmarks[0].metrics_schema" in the response should have length at least 1
    And the array at path "$.results.benchmarks[1].metrics_schema" in the response should have length at least 1
    And the array at path "$.results.benchmarks[2].metrics_schema" in the response should have length at least 1
    And the array at path "$.results.benchmarks[3].metrics_schema" in the response should have length at least 1
    And the array at path "$.results.benchmarks[4].metrics_schema" in the response should have length at least 1
    And the array at path "$.results.benchmarks[5].metrics_schema" in the response should have length at least 1
    And the array at path "$.results.benchmarks[6].metrics_schema" in the response should have length at least 1
    And the array at path "$.results.benchmarks[7].metrics_schema" in the response should have length at least 1
    And the array at path "$.results.benchmarks[8].metrics_schema" in the response should have length at least 1
    And the array at path "$.results.benchmarks[9].metrics_schema" in the response should have length at least 1
    And the array at path "$.results.benchmarks[10].metrics_schema" in the response should have length at least 1
    And the array at path "$.results.benchmarks[11].metrics_schema" in the response should have length at least 1
    And the array at path "$.results.benchmarks[12].metrics_schema" in the response should have length at least 1
    And the array at path "$.results.benchmarks[13].metrics_schema" in the response should have length at least 1
    And the array at path "$.results.benchmarks[14].metrics_schema" in the response should have length at least 1
    And the array at path "$.results.benchmarks[15].metrics_schema" in the response should have length at least 1
    And the array at path "$.results.benchmarks[16].metrics_schema" in the response should have length at least 1
    And the array at path "$.results.benchmarks[17].metrics_schema" in the response should have length at least 1
    And the array at path "$.results.benchmarks[18].metrics_schema" in the response should have length at least 1
    And the array at path "$.results.benchmarks[19].metrics_schema" in the response should have length at least 1
    And the array at path "$.results.benchmarks[20].metrics_schema" in the response should have length at least 1
    And the array at path "$.results.benchmarks[21].metrics_schema" in the response should have length at least 1
    And the array at path "$.results.benchmarks[22].metrics_schema" in the response should have length at least 1
    And the array at path "$.results.benchmarks[23].metrics_schema" in the response should have length at least 1
    And the array at path "$.results.benchmarks[24].metrics_schema" in the response should have length at least 1
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
    # TODO: Add per-benchmark status and metrics_schema checks once resolved: RHOAIENG-85386, RHOAIENG-85389, RHOAIENG-85388
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
    # TODO: Add per-benchmark status and metrics_schema checks once resolved: RHOAIENG-85389, RHOAIENG-85386
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
    And the response should contain the value "completed" at path "$.status.benchmarks[?(@.id == &quot;AraDiCE_ArabicMMLU_middle_social-science_civics_egy&quot;)].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[?(@.id == &quot;AraDiCE_ArabicMMLU_middle_social-science_economics_lev&quot;)].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[?(@.id == &quot;AraDiCE_ArabicMMLU_middle_social-science_social-science_egy&quot;)].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[?(@.id == &quot;AraDiCE_ArabicMMLU_middle_stem_computer-science_lev&quot;)].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[?(@.id == &quot;AraDiCE_ArabicMMLU_primary_social-science_geography_egy&quot;)].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[?(@.id == &quot;AraDiCE_ArabicMMLU_primary_social-science_social-science_lev&quot;)].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[?(@.id == &quot;AraDiCE_ArabicMMLU_primary_stem_natural-science_lev&quot;)].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[?(@.id == &quot;AraDiCE_ArabicMMLU_univ_social-science_accounting_lev&quot;)].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[?(@.id == &quot;AraDiCE_ArabicMMLU_univ_social-science_political-science_egy&quot;)].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[?(@.id == &quot;AraDiCE_ArabicMMLU_univ_stem_computer-science_lev&quot;)].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[?(@.id == &quot;arabic_leaderboard_arabic_mmlu_college_biology_light&quot;)].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[?(@.id == &quot;agieval_logiqa_zh&quot;)].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[?(@.id == &quot;bbh&quot;)].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[?(@.id == &quot;bbh_cot_fewshot&quot;)].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[?(@.id == &quot;bbh_cot_fewshot_causal_judgement&quot;)].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[?(@.id == &quot;bbh_cot_fewshot_dyck_languages&quot;)].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[?(@.id == &quot;bbh_cot_fewshot_hyperbaton&quot;)].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[?(@.id == &quot;bbh_cot_fewshot_logical_deduction_three_objects&quot;)].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[?(@.id == &quot;bbh_cot_fewshot_navigate&quot;)].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[?(@.id == &quot;bbh_cot_fewshot_reasoning_about_colored_objects&quot;)].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[?(@.id == &quot;bbh_cot_fewshot_snarks&quot;)].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[?(@.id == &quot;bbh_cot_fewshot_tracking_shuffled_objects_five_objects&quot;)].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[?(@.id == &quot;bbh_cot_fewshot_web_of_lies&quot;)].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[?(@.id == &quot;bbh_cot_zeroshot&quot;)].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[?(@.id == &quot;bbh_cot_zeroshot_causal_judgement&quot;)].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[?(@.id == &quot;bbh_cot_zeroshot_dyck_languages&quot;)].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[?(@.id == &quot;arabic_leaderboard_arabic_mmlu_anatomy&quot;)].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[?(@.id == &quot;arabic_leaderboard_arabic_mmlu_clinical_knowledge&quot;)].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[?(@.id == &quot;arabic_leaderboard_arabic_mmlu_medical_genetics&quot;)].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[?(@.id == &quot;arabic_leaderboard_arabic_mmlu_professional_medicine&quot;)].status"
    And the array at path "$.results.benchmarks[0].metrics_schema" in the response should have length at least 1
    And the array at path "$.results.benchmarks[1].metrics_schema" in the response should have length at least 1
    And the array at path "$.results.benchmarks[2].metrics_schema" in the response should have length at least 1
    And the array at path "$.results.benchmarks[3].metrics_schema" in the response should have length at least 1
    And the array at path "$.results.benchmarks[4].metrics_schema" in the response should have length at least 1
    And the array at path "$.results.benchmarks[5].metrics_schema" in the response should have length at least 1
    And the array at path "$.results.benchmarks[6].metrics_schema" in the response should have length at least 1
    And the array at path "$.results.benchmarks[7].metrics_schema" in the response should have length at least 1
    And the array at path "$.results.benchmarks[8].metrics_schema" in the response should have length at least 1
    And the array at path "$.results.benchmarks[9].metrics_schema" in the response should have length at least 1
    And the array at path "$.results.benchmarks[10].metrics_schema" in the response should have length at least 1
    And the array at path "$.results.benchmarks[11].metrics_schema" in the response should have length at least 1
    And the array at path "$.results.benchmarks[12].metrics_schema" in the response should have length at least 1
    And the array at path "$.results.benchmarks[13].metrics_schema" in the response should have length at least 1
    And the array at path "$.results.benchmarks[14].metrics_schema" in the response should have length at least 1
    And the array at path "$.results.benchmarks[15].metrics_schema" in the response should have length at least 1
    And the array at path "$.results.benchmarks[16].metrics_schema" in the response should have length at least 1
    And the array at path "$.results.benchmarks[17].metrics_schema" in the response should have length at least 1
    And the array at path "$.results.benchmarks[18].metrics_schema" in the response should have length at least 1
    And the array at path "$.results.benchmarks[19].metrics_schema" in the response should have length at least 1
    And the array at path "$.results.benchmarks[20].metrics_schema" in the response should have length at least 1
    And the array at path "$.results.benchmarks[21].metrics_schema" in the response should have length at least 1
    And the array at path "$.results.benchmarks[22].metrics_schema" in the response should have length at least 1
    And the array at path "$.results.benchmarks[23].metrics_schema" in the response should have length at least 1
    And the array at path "$.results.benchmarks[24].metrics_schema" in the response should have length at least 1
    And the array at path "$.results.benchmarks[25].metrics_schema" in the response should have length at least 1
    And the array at path "$.results.benchmarks[26].metrics_schema" in the response should have length at least 1
    And the array at path "$.results.benchmarks[27].metrics_schema" in the response should have length at least 1
    And the array at path "$.results.benchmarks[28].metrics_schema" in the response should have length at least 1
    And the array at path "$.results.benchmarks[29].metrics_schema" in the response should have length at least 1
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
    # TODO: Add per-benchmark status and metrics_schema checks once resolved: RHOAIENG-85389, RHOAIENG-85386, RHOAIENG-85388, RHOAIENG-85393
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
    # TODO: Add per-benchmark status and metrics_schema checks once resolved: RHOAIENG-85386, RHOAIENG-85388, RHOAIENG-85393, RHOAIENG-85410
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
    And the response should contain the value "completed" at path "$.status.benchmarks[?(@.id == &quot;blimp&quot;)].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[?(@.id == &quot;AraDiCE_ArabicMMLU_na_other_general-knowledge_lev&quot;)].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[?(@.id == &quot;AraDiCE_ArabicMMLU_primary_humanities_islamic-studies_egy&quot;)].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[?(@.id == &quot;AraDiCE_ArabicMMLU_primary_language_arabic-language_lev&quot;)].status"
    And the response should contain the value "completed" at path "$.status.benchmarks[?(@.id == &quot;AraDiCE_ArabicMMLU_univ_other_management_egy&quot;)].status"
    And the array at path "$.results.benchmarks[0].metrics_schema" in the response should have length at least 1
    And the array at path "$.results.benchmarks[1].metrics_schema" in the response should have length at least 1
    And the array at path "$.results.benchmarks[2].metrics_schema" in the response should have length at least 1
    And the array at path "$.results.benchmarks[3].metrics_schema" in the response should have length at least 1
    And the array at path "$.results.benchmarks[4].metrics_schema" in the response should have length at least 1
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
    # TODO: Add per-benchmark status and metrics_schema checks once resolved: RHOAIENG-89382, RHOAIENG-89395
    When I send a DELETE request to "/api/v1/evaluations/jobs/{id}?hard_delete=true"
    Then the response code should be 204

  # MTEB not implemented due to https://redhat.atlassian.net/browse/RHOAIENG-85265
  @ignore
  Scenario: Verifying results returned for Evaluation job - MTEB
    Given the service is running
