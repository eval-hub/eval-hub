@cluster
@non-admin
@non-admin_viewer

Feature: Evaluation Hub RBAC for a viewer
  As a data scientist with a viewer role
  I want to read evaluation resources without modifying them
  So that viewer access is enforced by the cluster RBAC configuration

  Background:
    Given I set the header "X-Tenant" to "{{env:X_TENANT|test-tenant}}"
    And I set the header "X-User" to "{{env:X_USER|test-viewer}}"
    And I set the wait deadline to "{{env:WAIT_DEADLINE|30m}}"

  Scenario: Viewer can list evaluation jobs
    Given the service is running
    When I send a GET request to "/api/v1/evaluations/jobs"
    Then the response code should be 200

  Scenario: Viewer can get an existing evaluation job
    Given the service is running
    And I set the resource ID from environment variable "VIEWER_JOB_ID"
    When I send a GET request to "/api/v1/evaluations/jobs/{id}"
    Then the response code should be 200

  @negative
  Scenario: Viewer cannot create an evaluation job
    Given the service is running
    # Malformed JSON makes an unexpected authorization grant fail before a job can be created.
    When I send a POST request to "/api/v1/evaluations/jobs" with body:
    """
    {
    """
    Then the response code should be 403
    And the response body should contain "user={{env:X_USER|test-viewer}}, verb=create, resource=evaluations"

  @negative
  Scenario: Viewer cannot delete an evaluation job
    Given the service is running
    # Target a nonexistent ID so a permission regression cannot delete a real tenant job.
    When I send a DELETE request to "/api/v1/evaluations/jobs/00000000-0000-0000-0000-000000000000?hard_delete=true"
    Then the response code should be 403
    And the response body should contain "user={{env:X_USER|test-viewer}}, verb=delete, resource=evaluations"

  @negative
  Scenario: Viewer cannot post evaluation status events
    Given the service is running
    # Use a nonexistent job ID so an unexpected authorization grant cannot mutate a real job.
    When I send a POST request to "/api/v1/evaluations/jobs/00000000-0000-0000-0000-000000000000/events" with body "file:/evaluation_job_status_event_running.json"
    Then the response code should be 403
    And the response body should contain "user={{env:X_USER|test-viewer}}, verb=create, resource=status-events"

  @mlflow
  @negative
  Scenario: Viewer cannot get, list, create, update, or delete MLflow experiments
    Given the service is running
    And I set the resource ID from environment variable "VIEWER_MLFLOW_EXPERIMENT_ID"
    Then the viewer cannot get, list, create, update, or delete MLflow experiments

  Scenario: Viewer can list collections
    Given the service is running
    When I send a GET request to "/api/v1/evaluations/collections"
    Then the response code should be 200

  @negative
  Scenario: Viewer cannot create a collection
    Given the service is running
    # Malformed JSON makes an unexpected authorization grant fail before a resource can be created.
    When I send a POST request to "/api/v1/evaluations/collections" with body:
    """
    {
    """
    Then the response code should be 403
    And the response body should contain "user={{env:X_USER|test-viewer}}, verb=create, resource=collections"

  Scenario: Viewer can get an existing collection
    Given the service is running
    And I set the resource ID from environment variable "VIEWER_COLLECTION_ID"
    When I send a GET request to "/api/v1/evaluations/collections/{id}"
    Then the response code should be 200

  @negative
  Scenario: Viewer cannot replace a collection
    Given the service is running
    # Target a nonexistent ID so a permission regression cannot alter a shared collection.
    When I send a PUT request to "/api/v1/evaluations/collections/00000000-0000-0000-0000-000000000000" with body "file:/collection.json"
    Then the response code should be 403
    And the response body should contain "user={{env:X_USER|test-viewer}}, verb=update, resource=collections"

  @negative
  Scenario: Viewer cannot patch a collection
    Given the service is running
    # Target a nonexistent ID so a permission regression cannot alter a shared collection.
    When I send a PATCH request to "/api/v1/evaluations/collections/00000000-0000-0000-0000-000000000000" with body "file:/patch_collection_name.json"
    Then the response code should be 403
    And the response body should contain "user={{env:X_USER|test-viewer}}, verb=patch, resource=collections"

  @negative
  Scenario: Viewer cannot delete a collection
    Given the service is running
    # Target a nonexistent ID so a permission regression cannot delete a shared collection.
    When I send a DELETE request to "/api/v1/evaluations/collections/00000000-0000-0000-0000-000000000000"
    Then the response code should be 403
    And the response body should contain "user={{env:X_USER|test-viewer}}, verb=delete, resource=collections"

  Scenario: Viewer can list providers
    Given the service is running
    When I send a GET request to "/api/v1/evaluations/providers"
    Then the response code should be 200

  @negative
  Scenario: Viewer cannot create a provider
    Given the service is running
    # Malformed JSON makes an unexpected authorization grant fail before a resource can be created.
    When I send a POST request to "/api/v1/evaluations/providers" with body:
    """
    {
    """
    Then the response code should be 403
    And the response body should contain "user={{env:X_USER|test-viewer}}, verb=create, resource=providers"

  Scenario: Viewer can get an existing provider
    Given the service is running
    And I set the resource ID from environment variable "VIEWER_PROVIDER_ID"
    When I send a GET request to "/api/v1/evaluations/providers/{id}"
    Then the response code should be 200

  @negative
  Scenario: Viewer cannot update a provider
    Given the service is running
    # Target a nonexistent ID so a permission regression cannot alter a shared provider.
    When I send a PUT request to "/api/v1/evaluations/providers/00000000-0000-0000-0000-000000000000" with body "file:/user_provider_with_agent_metadata_full.json"
    Then the response code should be 403
    And the response body should contain "user={{env:X_USER|test-viewer}}, verb=update, resource=providers"

  @negative
  Scenario: Viewer cannot patch a provider
    Given the service is running
    # Target a nonexistent ID so a permission regression cannot alter a shared provider.
    When I send a PATCH request to "/api/v1/evaluations/providers/00000000-0000-0000-0000-000000000000" with body "file:/user_provider_with_agent_metadata_full.json"
    Then the response code should be 403
    And the response body should contain "user={{env:X_USER|test-viewer}}, verb=patch, resource=providers"

  @negative
  Scenario: Viewer cannot delete a provider
    Given the service is running
    # Target a nonexistent ID so a permission regression cannot delete a shared provider.
    When I send a DELETE request to "/api/v1/evaluations/providers/00000000-0000-0000-0000-000000000000"
    Then the response code should be 403
    And the response body should contain "user={{env:X_USER|test-viewer}}, verb=delete, resource=providers"
