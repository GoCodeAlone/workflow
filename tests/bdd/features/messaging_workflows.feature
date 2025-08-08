Feature: Messaging Workflow Testing
  As a workflow developer
  I want to create and test messaging-based workflows
  So that I can build event-driven and message processing systems

  Background:
    Given the workflow UI is running
    And I am logged in as an admin user

  Scenario: Create a basic messaging workflow
    When I create a messaging workflow with:
      | topic   | user-events |
      | handler | user-handler |
    Then the workflow should be created successfully
    And the workflow should use "messaging.broker" module
    And the workflow should process messages

  Scenario: Create multi-topic messaging workflow
    When I create a messaging workflow with:
      | topic   | user-events |
      | topic   | order-events |
      | topic   | notification-events |
      | handler | user-handler |
      | handler | order-handler |
      | handler | notification-handler |
    Then the workflow should be created successfully
    And the workflow should process messages

  Scenario: Create event processing pipeline
    When I create a workflow with:
      | name        | Event Processing Pipeline |
      | description | Multi-stage event processing workflow |
      | config_file | event_processing_pipeline.yaml |
    Then the workflow should be created successfully
    And the workflow should process messages

  Scenario: Create event-driven microservices workflow
    When I create a workflow with:
      | name        | Event-Driven Microservices |
      | description | Microservices communicating via events |
      | config_file | event_driven_microservices.yaml |
    Then the workflow should be created successfully
    And the workflow should process messages

  Scenario: Create message routing workflow
    When I create a workflow with:
      | name        | Message Routing Workflow |
      | description | Route messages based on content |
      | config_file | message_routing_workflow.yaml |
    Then the workflow should be created successfully
    And the workflow should process messages

  Scenario: Create pub-sub notification workflow
    When I create a workflow with:
      | name        | Pub-Sub Notification Workflow |
      | description | Publisher-subscriber notification system |
      | config_file | pubsub_notification_workflow.yaml |
    Then the workflow should be created successfully
    And the workflow should process messages

  Scenario: Create dead letter queue workflow
    When I create a workflow with:
      | name        | Dead Letter Queue Workflow |
      | description | Handle failed message processing |
      | config_file | dead_letter_queue_workflow.yaml |
    Then the workflow should be created successfully
    And the workflow should process messages