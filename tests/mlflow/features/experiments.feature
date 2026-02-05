Feature: MLflow Experiments API
  As a developer
  I want to manage MLflow experiments
  So that I can organize my machine learning runs
  Note that you can not delete an experiment and then immediately create a
  new one with the same name because it may not yet be completely deleted

  Background:
    Given an MLflow server is running
    And I have an MLflow client connected to the server

  Scenario: Create a new experiment
    When I create an experiment named "test-experiments"
    Then the experiment should be created successfully
    And the experiment should have the name "test-experiments"

  Scenario: Get an experiment by ID
    When I create an experiment named "get-experiment"
    And an experiment named "get-experiment" exists
    When I get the experiment by ID
    Then the experiment should be returned
    And the experiment should have the name "get-experiment"

  Scenario: Get an experiment by name
    When I create an experiment named "get-by-name-experiment"
    And an experiment named "get-by-name-experiment" exists
    When I get the experiment by name "get-by-name-experiment"
    Then the experiment should be returned
    And the experiment should have the name "get-by-name-experiment"

  Scenario: Delete an experiment
    Given an experiment named "delete-test" exists
    When I delete the experiment
