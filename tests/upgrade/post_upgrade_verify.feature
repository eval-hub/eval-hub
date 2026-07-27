@post-upgrade-verify
Feature: Post-Upgrade Verify — Fixtures Survived
  Verify that resources created before the upgrade still exist and
  are retrievable on the upgraded cluster.

  Background:
    Given I set the header "Authorization" to "Bearer {{env:AUTH_TOKEN}}"
    And I set the header "X-Tenant" to "{{env:X_TENANT}}"
    And I set the header "X-User" to "{{env:X_USER|upgrade-test-user}}"
    And I load the upgrade state from "{{env:UPGRADE_STATE_JSON}}"

  Scenario: All pre-upgrade jobs still exist
    Given the service is running
    Then I verify all jobs from upgrade state exist
