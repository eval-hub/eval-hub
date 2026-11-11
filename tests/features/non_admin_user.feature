@cluster
@non-admin
@non-admin_user

Feature: Evaluation Hub RBAC for an evalhub user
  As a data scientist with an evalhub-user role
  I want to run evaluation jobs while keeping collections and providers read-only
  So that the intended non-admin permissions are enforced

  Background:
    Given I set the header "X-Tenant" to "{{env:X_TENANT|test-tenant}}"
    And I set the header "X-User" to "{{env:X_USER|test-user}}"
    And I set the wait deadline to "{{env:WAIT_DEADLINE|30m}}"

  Scenario: User can list evaluation jobs
    Given the service is running
    When I send a GET request to "/api/v1/evaluations/jobs"
    Then the response code should be 200

  @mlflow
  Scenario: User can create, get, and list MLflow experiments but cannot update or delete them
    Given the service is running
    When I create an MLflow experiment as the current user
    Then the current user can get the created MLflow experiment
    And the current user can list the created MLflow experiment
    And the current user cannot update the created MLflow experiment
    And the current user cannot delete the created MLflow experiment
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job.json"
    Then the response code should be 202
    And the response should contain the value "my-test-experiment" at path "$.experiment.name"
    And the response should match the value "^[0-9]+$" at path "$.resource.mlflow_experiment_id"
    And the "resource.mlflow_experiment_id" field in the response should be saved as "value:user_mlflow_experiment_id"
    When I send a GET request to "/api/v1/evaluations/jobs/{id}"
    Then the response code should be 200
    And the response should contain the value "my-test-experiment" at path "$.experiment.name"
    And the response should contain the value "{{value:user_mlflow_experiment_id}}" at path "$.resource.mlflow_experiment_id"
    When I send a DELETE request to "/api/v1/evaluations/jobs/{id}?hard_delete=true"
    Then the response code should be 204

  Scenario: User can create and delete a completed evaluation job but cannot update its status
    Given the service is running
    When I send a POST request to "/api/v1/evaluations/jobs" with body "file:/evaluation_job.json"
    Then the response code should be 202
    And the response should contain the value "{{env:X_USER|test-user}}" at path "$.resource.owner"
    When I send a GET request to "/api/v1/evaluations/jobs/{id}"
    Then the response code should be 200
    When I send a POST request to "/api/v1/evaluations/jobs/{id}/events" with body "file:/evaluation_job_status_event_running.json"
    Then the response code should be 403
    And the response body should contain "user={{env:X_USER|test-user}}, verb=create, resource=status-events"
    And I wait for the evaluation job status to be "completed"
    When I send a DELETE request to "/api/v1/evaluations/jobs/{id}?hard_delete=true"
    Then the response code should be 204

  Scenario: User can list collections
    Given the service is running
    When I send a GET request to "/api/v1/evaluations/collections"
    Then the response code should be 200

  Scenario: User can get an existing collection
    Given the service is running
    And I set the resource ID from environment variable "USER_COLLECTION_ID"
    When I send a GET request to "/api/v1/evaluations/collections/{id}"
    Then the response code should be 200

  @negative
  Scenario: User cannot create a collection
    Given the service is running
    # Malformed JSON makes an unexpected authorization grant fail before a resource can be created.
    When I send a POST request to "/api/v1/evaluations/collections" with body:
    """
    {
    """
    Then the response code should be 403
    And the response body should contain "user={{env:X_USER|test-user}}, verb=create, resource=collections"

  @negative
  Scenario: User cannot replace a collection
    Given the service is running
    # Target a nonexistent ID so a permission regression cannot alter a shared collection.
    When I send a PUT request to "/api/v1/evaluations/collections/00000000-0000-0000-0000-000000000000" with body "file:/collection.json"
    Then the response code should be 403
    And the response body should contain "user={{env:X_USER|test-user}}, verb=update, resource=collections"

  @negative
  Scenario: User cannot patch a collection
    Given the service is running
    # Target a nonexistent ID so a permission regression cannot alter a shared collection.
    When I send a PATCH request to "/api/v1/evaluations/collections/00000000-0000-0000-0000-000000000000" with body "file:/patch_collection_name.json"
    Then the response code should be 403
    And the response body should contain "user={{env:X_USER|test-user}}, verb=patch, resource=collections"

  @negative
  Scenario: User cannot delete a collection
    Given the service is running
    # Target a nonexistent ID so a permission regression cannot delete a shared collection.
    When I send a DELETE request to "/api/v1/evaluations/collections/00000000-0000-0000-0000-000000000000"
    Then the response code should be 403
    And the response body should contain "user={{env:X_USER|test-user}}, verb=delete, resource=collections"

  Scenario: User can list providers
    Given the service is running
    When I send a GET request to "/api/v1/evaluations/providers"
    Then the response code should be 200

  Scenario: User can get an existing provider
    Given the service is running
    And I set the resource ID from environment variable "USER_PROVIDER_ID"
    When I send a GET request to "/api/v1/evaluations/providers/{id}"
    Then the response code should be 200

  @negative
  Scenario: User cannot create a provider
    Given the service is running
    # Malformed JSON makes an unexpected authorization grant fail before a resource can be created.
    When I send a POST request to "/api/v1/evaluations/providers" with body:
    """
    {
    """
    Then the response code should be 403
    And the response body should contain "user={{env:X_USER|test-user}}, verb=create, resource=providers"

  @negative
  Scenario: User cannot update a provider
    Given the service is running
    # Target a nonexistent ID so a permission regression cannot alter a shared provider.
    When I send a PUT request to "/api/v1/evaluations/providers/00000000-0000-0000-0000-000000000000" with body "file:/user_provider_update.json"
    Then the response code should be 403
    And the response body should contain "user={{env:X_USER|test-user}}, verb=update, resource=providers"

  @negative
  Scenario: User cannot patch a provider
    Given the service is running
    # Target a nonexistent ID so a permission regression cannot alter a shared provider.
    When I send a PATCH request to "/api/v1/evaluations/providers/00000000-0000-0000-0000-000000000000" with body "file:/user_provider_patch.json"
    Then the response code should be 403
    And the response body should contain "user={{env:X_USER|test-user}}, verb=patch, resource=providers"

  @negative
  Scenario: User cannot delete a provider
    Given the service is running
    # Target a nonexistent ID so a permission regression cannot delete a shared provider.
    When I send a DELETE request to "/api/v1/evaluations/providers/00000000-0000-0000-0000-000000000000"
    Then the response code should be 403
    And the response body should contain "user={{env:X_USER|test-user}}, verb=delete, resource=providers"
