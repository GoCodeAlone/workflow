Feature: State Machine Workflow Testing
  As a workflow developer
  I want to create and test state machine-based workflows
  So that I can build complex business process automation with state transitions

  Background:
    Given the workflow UI is running
    And I am logged in as an admin user

  Scenario: Create a basic order processing state machine
    When I create a state machine workflow with:
      | initialState | new |
      | states       | new,processing,completed,failed |
    Then the workflow should be created successfully
    And the workflow should use "statemachine.engine" module
    And the workflow should manage state transitions

  Scenario: Create user onboarding state machine workflow
    When I create a workflow with:
      | name        | User Onboarding State Machine |
      | description | Manages user onboarding process |
      | config_file | user_onboarding_statemachine.yaml |
    Then the workflow should be created successfully
    And the workflow should manage state transitions

  Scenario: Create order fulfillment state machine workflow
    When I create a workflow with:
      | name        | Order Fulfillment State Machine |
      | description | E-commerce order processing workflow |
      | config_file | order_fulfillment_statemachine.yaml |
    Then the workflow should be created successfully
    And the workflow should manage state transitions

  Scenario: Create loan approval state machine workflow
    When I create a workflow with:
      | name        | Loan Approval State Machine |
      | description | Bank loan approval process |
      | config_file | loan_approval_statemachine.yaml |
    Then the workflow should be created successfully
    And the workflow should manage state transitions

  Scenario: Create incident management state machine workflow
    When I create a workflow with:
      | name        | Incident Management State Machine |
      | description | IT incident management workflow |
      | config_file | incident_management_statemachine.yaml |
    Then the workflow should be created successfully
    And the workflow should manage state transitions

  Scenario: Create document approval state machine workflow
    When I create a workflow with:
      | name        | Document Approval State Machine |
      | description | Document review and approval process |
      | config_file | document_approval_statemachine.yaml |
    Then the workflow should be created successfully
    And the workflow should manage state transitions