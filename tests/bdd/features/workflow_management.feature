Feature: Workflow Management
  As an authenticated user
  I want to manage workflows through the UI
  So that I can create, edit, execute, and monitor workflows

  Background:
    Given the workflow UI is running
    And I am logged in as an admin user

  Scenario: Create a new workflow
    When I create a workflow with:
      | name        | Test Workflow           |
      | description | A simple test workflow  |
      | config_file | basic_workflow.yaml     |
    Then the workflow should be created successfully
    And I should be able to retrieve the workflow

  Scenario: List workflows
    Given there are existing workflows
    When I request the list of workflows
    Then I should receive all workflows for my tenant
    And each workflow should have id, name, description, and status

  Scenario: Update a workflow
    Given there is a workflow named "Test Workflow"
    When I update the workflow with:
      | name        | Updated Test Workflow   |
      | description | An updated description  |
    Then the workflow should be updated successfully
    And the changes should be reflected in the workflow details

  Scenario: Execute a workflow
    Given there is a workflow named "Test Workflow"
    When I execute the workflow with input data
    Then a workflow execution should be created
    And the execution should have status "running" initially

  Scenario: View workflow executions
    Given there is a workflow with executions
    When I request the executions for the workflow
    Then I should receive all executions for that workflow
    And each execution should have id, status, start time, and logs

  Scenario: Delete a workflow
    Given there is a workflow named "Test Workflow"
    When I delete the workflow
    Then the workflow should be marked as inactive
    And it should not appear in the active workflows list