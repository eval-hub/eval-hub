@cluster @collection_generation
Feature: Collection generation
  A tenant can generate a dry-run collection proposal and track it independently of evaluation jobs.

  # Set GODOG_PATHS=tests/features/collection_generation.feature and select
  # GODOG_TAGS=@collection_generation_live against a configured server. Set SERVER_URL, X_TENANT,
  # AUTH_TOKEN, FVT_GENERATION_MODEL_NAME, and FVT_GENERATION_MODEL_URL. Set
  # FVT_GENERATION_MODEL_SECRET_REF if the generator requires a tenant Secret.
  # Use a controlled catalog/model fixture for stable selection and ranking. The single-provider
  # case needs at least three eligible Inspect benchmarks and FVT_GENERATION_REJECTED_IDS in rank
  # order. The multi-provider case needs eligible Inspect, Lighteval, and out-of-filter candidates.
  # The caller needs API access to the fixture and Kubernetes list access to Jobs.
  Background:
    Given I set the header "X-Tenant" to "{{env:X_TENANT|test-tenant}}"
    And I set the header "X-User" to "{{env:X_USER|test-user}}"
    And I set the header "Content-Type" to "application/json"
    And I set the wait deadline to "{{env:FVT_GENERATION_WAIT_DEADLINE|10m}}"
    And I set the wait interval to "{{env:FVT_GENERATION_WAIT_INTERVAL|2s}}"

  # Run against an unconfigured server with FVT_GENERATION_UNCONFIGURED=true.
  @collection_generation_unconfigured @negative
  Scenario: Valid generation is unavailable when the feature is unconfigured
    Given the unconfigured generation target is selected
    When I send a POST request to "/api/v1/evaluations/collections/generation" with body:
      """
      {"evaluation_goal":"Assess math reasoning","model":{"name":"test-generator","url":"https://generator.example.invalid/v1"}}
      """
    Then the collection generation request should be rejected with 503
    And the response should equal the value "collection_generation_not_configured" at path "message_code"

  @collection_generation_live
  Rule: A configured service admits valid requests and exposes their durable outcome

    Background:
      Given the live collection generation fixture is configured

    @positive
    Scenario: Generate with default options and create the proposal separately
      When I submit a collection generation request with:
        """
        {"evaluation_goal":"  Assess grade-school math reasoning  "}
        """
      Then the collection generation request should be admitted
      When I poll the generation Location until it is "completed"
      Then the response should equal the value "Assess grade-school math reasoning" at path "request.evaluation_goal"
      And the response should equal the value "12" at path "request.max_benchmarks"
      And the response should equal the value "moderate" at path "request.strictness"
      And the response should equal the value "standard" at path "request.generation_effort"
      And the generation should use all visible providers
      And the response at path "result.collection.name" should not be empty
      And the response at path "result.collection.description" should not be empty
      And the generated proposal should match the collection creation contract
      And the completed generation should remain readable after its pod exits
      And the generation should leave no collection or ordinary evaluation job
      When I submit the proposal as a separate collection
      Then the response code should be 200

    @positive
    Scenario: Enforce a single-provider filter and a cap of two
      Given the value "{{env:FVT_GENERATION_REJECTED_IDS}}" is not empty
      When I submit a collection generation request with:
        """
        {
          "evaluation_goal":"Compare math and general reasoning using Inspect benchmarks",
          "collection_name":"  FVT Inspect reasoning  ",
          "collection_description":"  Evaluate math and general reasoning.  ",
          "provider_filter":"inspect",
          "max_benchmarks":2,
          "strictness":"lenient",
          "generation_effort":"low"
        }
        """
      Then the collection generation request should be admitted
      When I poll the generation Location until it is "completed"
      Then the response should equal the value "inspect" at path "request.provider_filter"
      And the response should equal the value "2" at path "request.max_benchmarks"
      And the response should equal the value "lenient" at path "request.strictness"
      And the response should equal the value "low" at path "request.generation_effort"
      And the response should equal the value "FVT Inspect reasoning" at path "result.collection.name"
      And the response should equal the value "Evaluate math and general reasoning." at path "result.collection.description"
      And the array at path "result.collection.benchmarks" in the response should have length 2
      And the generated proposal should match the collection creation contract
      And the rejected generation benchmarks should be ordered as "{{env:FVT_GENERATION_REJECTED_IDS}}"

    # Provide a second same-tenant caller and a caller authorized in a different tenant.
    # Set FVT_GENERATION_READER_TOKEN, FVT_GENERATION_OTHER_TENANT and
    # FVT_GENERATION_OTHER_TENANT_TOKEN.
    @positive
    Scenario: Generate across two providers at the upper cap and enforce tenant visibility
      When I submit a collection generation request with:
        """
        {
          "evaluation_goal":"Compare math reasoning using benchmarks from both Inspect and Lighteval",
          "provider_filter":" inspect , lighteval ",
          "max_benchmarks":100,
          "strictness":"strict",
          "generation_effort":"high"
        }
        """
      Then the collection generation request should be admitted
      When I poll the generation Location until it is "completed"
      Then the response should equal the value "inspect,lighteval" at path "request.provider_filter"
      And the response should equal the value "100" at path "request.max_benchmarks"
      And the response should equal the value "strict" at path "request.strictness"
      And the response should equal the value "high" at path "request.generation_effort"
      And the generated proposal should match the collection creation contract
      And the generated proposal should select benchmarks from both "inspect" and "lighteval"
      When I send a GET generation request as "same-tenant reader"
      Then the response code should be 200
      And the response should equal the value "completed" at path "status.state"
      And the response should equal the value "{{value:id}}" at path "resource.id"
      When I send a GET generation request as "other tenant"
      Then the response code should be 404
      When I send a DELETE generation request as "other tenant"
      Then the response code should be 404
      When I send a GET request to "/api/v1/evaluations/collections/generation/{id}"
      Then the response code should be 200
      When I send a DELETE request to "/api/v1/evaluations/collections/generation/{id}"
      Then the response code should be 204
      When I send a GET request to "/api/v1/evaluations/collections/generation/{id}"
      Then the response code should be 404
      When I send a DELETE request to "/api/v1/evaluations/collections/generation/{id}"
      Then the response code should be 404

    @positive @collection_generation_guardrail
    Scenario: The guardrail rejects an admitted off-topic goal
      When I submit a collection generation request with:
        """
        {"evaluation_goal":"Write a limerick about a sourdough starter."}
        """
      Then the collection generation request should be admitted
      When I poll the generation Location until it is "failed"
      Then the guardrail should reject generation with category "topic_relevance"

    @negative
    Scenario Outline: Reject an invalid generation request before admission
      When I submit a collection generation request with:
        """
        {<fields>}
        """
      Then the collection generation request should be rejected with 400

      Examples:
        | case                  | fields                                                                                                      |
        | no goal               |                                                                                                             |
        | blank goal            | "evaluation_goal":"  "                                                                                      |
        | null goal             | "evaluation_goal":null                                                                                      |
        | wrong goal type       | "evaluation_goal":1                                                                                         |
        | blank name            | "evaluation_goal":"Assess reasoning","collection_name":" "                                                   |
        | blank description     | "evaluation_goal":"Assess reasoning","collection_description":" "                                            |
        | no model URL          | "evaluation_goal":"Assess reasoning","model":{"name":"generator"}                                            |
        | no model name         | "evaluation_goal":"Assess reasoning","model":{"url":"https://generator.example.invalid/v1"}                 |
        | invalid model URL     | "evaluation_goal":"Assess reasoning","model":{"name":"generator","url":"invalid"}                           |
        | inline credential     | "evaluation_goal":"Assess reasoning","model":{"name":"generator","url":"https://generator.example.invalid/v1","parameters":{"api_key":"fixture-value"}} |
        | missing tenant Secret | "evaluation_goal":"Assess reasoning","model":{"name":"generator","url":"https://generator.example.invalid/v1","auth":{"secret_ref":"fvt-generation-secret-does-not-exist"}} |
        | malformed filter      | "evaluation_goal":"Assess reasoning","provider_filter":"inspect,,lighteval"                                   |
        | wrong filter type     | "evaluation_goal":"Assess reasoning","provider_filter":["inspect"]                                           |
        | below minimum         | "evaluation_goal":"Assess reasoning","max_benchmarks":0                                                      |
        | above maximum         | "evaluation_goal":"Assess reasoning","max_benchmarks":101                                                    |
        | fractional cap        | "evaluation_goal":"Assess reasoning","max_benchmarks":2.5                                                    |
        | wrong cap type        | "evaluation_goal":"Assess reasoning","max_benchmarks":"2"                                                    |
        | unknown strictness    | "evaluation_goal":"Assess reasoning","strictness":"unsupported"                                              |
        | unknown effort        | "evaluation_goal":"Assess reasoning","generation_effort":"unsupported"                                       |
        | unknown field         | "evaluation_goal":"Assess reasoning","generator_model_id":"unknown"                                         |

    @negative
    Scenario Outline: Reject an overlong generation input
      Given I prepare a generation input of <length> characters
      When I submit a collection generation request with:
        """
        {<fields>}
        """
      Then the collection generation request should be rejected with 400

      Examples:
        | fields                                                                                    | length |
        | "evaluation_goal":"{{value:generation_long_input}}"                                       | 10001  |
        | "evaluation_goal":"Assess reasoning","collection_description":"{{value:generation_long_input}}" | 1025 |

    @negative
    Scenario Outline: Reject an invalid request body
      When I send a POST request to "/api/v1/evaluations/collections/generation" with body:
        """
        <body>
        """
      Then the collection generation request should be rejected with 400

      Examples:
        | body                                   |
        | {"evaluation_goal":                    |
        | []                                     |
        | {"evaluation_goal":"Assess reasoning"} |

    @negative
    Scenario: Unknown generation IDs are inaccessible
      When I send a GET request to "/api/v1/evaluations/collections/generation/00000000-0000-0000-0000-000000000000"
      Then the response code should be 404
      When I send a DELETE request to "/api/v1/evaluations/collections/generation/00000000-0000-0000-0000-000000000000"
      Then the response code should be 404

    # Supply an expired generation from a short-retention fixture in X_TENANT.
    @negative @collection_generation_expired
    Scenario: An expired generation cannot be read or deleted
      Given I set the resource ID from environment variable "FVT_GENERATION_EXPIRED_ID"
      When I send a GET request to "/api/v1/evaluations/collections/generation/{id}"
      Then the response code should be 404
      When I send a DELETE request to "/api/v1/evaluations/collections/generation/{id}"
      Then the response code should be 404

    # Supply an ordinary job already readable in X_TENANT. The scenario creates no job.
    @negative @collection_generation_wrong_type
    Scenario: An ordinary evaluation job cannot be read or deleted as generation
      Given I set the resource ID from environment variable "FVT_GENERATION_ORDINARY_JOB_ID"
      When I send a GET request to "/api/v1/evaluations/jobs/{id}"
      Then the response code should be 200
      When I send a GET request to "/api/v1/evaluations/collections/generation/{id}"
      Then the response code should be 404
      When I send a DELETE request to "/api/v1/evaluations/collections/generation/{id}"
      Then the response code should be 404
      When I send a GET request to "/api/v1/evaluations/jobs/{id}"
      Then the response code should be 200

    @negative
    Scenario: The proxy rejects unauthenticated generation requests
      When I send a GET generation request as "unauthenticated caller"
      Then the response code should be 401

    # Set FVT_GENERATION_FORBIDDEN_TOKEN to a caller without generation access in X_TENANT.
    @negative @collection_generation_forbidden
    Scenario: The proxy rejects generation requests without tenant authorization
      When I send a GET generation request as "forbidden caller"
      Then the response code should be 403
