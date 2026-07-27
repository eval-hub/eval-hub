@post-upgrade-cleanup
Feature: Post-Upgrade Cleanup
  Delete evaluation jobs whose names contain "-upgrade-" on the upgraded cluster.

  Background:
    Given I set the header "Authorization" to "Bearer {{env:AUTH_TOKEN}}"
    And I set the header "X-Tenant" to "{{env:X_TENANT}}"
    And I set the header "X-User" to "{{env:X_USER|upgrade-test-user}}"

  Scenario: Hard-delete all upgrade evaluation jobs
    Given the service is running
    Then I hard-delete all evaluation jobs containing "-upgrade-" in its job name
