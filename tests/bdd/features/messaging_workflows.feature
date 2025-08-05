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
      | config      | modules:\n  - name: event-broker\n    type: messaging.broker\n  - name: input-processor\n    type: messaging.handler\n  - name: validation-processor\n    type: messaging.handler\n  - name: transformation-processor\n    type: messaging.handler\n  - name: output-processor\n    type: messaging.handler\n\nworkflows:\n  messaging:\n    subscriptions:\n      - topic: raw-events\n        handler: input-processor\n      - topic: validated-events\n        handler: validation-processor\n      - topic: transformed-events\n        handler: transformation-processor\n      - topic: processed-events\n        handler: output-processor\n    producers:\n      - name: input-processor\n        forwardTo:\n          - validated-events\n      - name: validation-processor\n        forwardTo:\n          - transformed-events\n      - name: transformation-processor\n        forwardTo:\n          - processed-events |
    Then the workflow should be created successfully
    And the workflow should process messages

  Scenario: Create event-driven microservices workflow
    When I create a workflow with:
      | name        | Event-Driven Microservices |
      | description | Microservices communicating via events |
      | config      | modules:\n  - name: event-bus\n    type: messaging.broker\n  - name: user-service\n    type: messaging.handler\n  - name: order-service\n    type: messaging.handler\n  - name: payment-service\n    type: messaging.handler\n  - name: notification-service\n    type: messaging.handler\n  - name: audit-service\n    type: messaging.handler\n\nworkflows:\n  messaging:\n    subscriptions:\n      - topic: user.created\n        handler: notification-service\n      - topic: user.updated\n        handler: audit-service\n      - topic: order.placed\n        handler: payment-service\n      - topic: order.placed\n        handler: notification-service\n      - topic: payment.processed\n        handler: order-service\n      - topic: payment.failed\n        handler: notification-service\n    producers:\n      - name: user-service\n        forwardTo:\n          - user.created\n          - user.updated\n      - name: order-service\n        forwardTo:\n          - order.placed\n          - order.updated\n      - name: payment-service\n        forwardTo:\n          - payment.processed\n          - payment.failed |
    Then the workflow should be created successfully
    And the workflow should process messages

  Scenario: Create message routing workflow
    When I create a workflow with:
      | name        | Message Routing Workflow |
      | description | Route messages based on content |
      | config      | modules:\n  - name: message-broker\n    type: messaging.broker\n  - name: message-router\n    type: messaging.handler\n  - name: priority-handler\n    type: messaging.handler\n  - name: standard-handler\n    type: messaging.handler\n  - name: error-handler\n    type: messaging.handler\n\nworkflows:\n  messaging:\n    subscriptions:\n      - topic: incoming-messages\n        handler: message-router\n      - topic: priority-messages\n        handler: priority-handler\n      - topic: standard-messages\n        handler: standard-handler\n      - topic: error-messages\n        handler: error-handler\n    producers:\n      - name: message-router\n        forwardTo:\n          - priority-messages\n          - standard-messages\n          - error-messages |
    Then the workflow should be created successfully
    And the workflow should process messages

  Scenario: Create pub-sub notification workflow
    When I create a workflow with:
      | name        | Pub-Sub Notification Workflow |
      | description | Publisher-subscriber notification system |
      | config      | modules:\n  - name: notification-broker\n    type: messaging.broker\n  - name: email-publisher\n    type: messaging.handler\n  - name: sms-publisher\n    type: messaging.handler\n  - name: push-publisher\n    type: messaging.handler\n  - name: email-subscriber\n    type: messaging.handler\n  - name: sms-subscriber\n    type: messaging.handler\n  - name: push-subscriber\n    type: messaging.handler\n\nworkflows:\n  messaging:\n    subscriptions:\n      - topic: email-notifications\n        handler: email-subscriber\n      - topic: sms-notifications\n        handler: sms-subscriber\n      - topic: push-notifications\n        handler: push-subscriber\n    producers:\n      - name: email-publisher\n        forwardTo:\n          - email-notifications\n      - name: sms-publisher\n        forwardTo:\n          - sms-notifications\n      - name: push-publisher\n        forwardTo:\n          - push-notifications |
    Then the workflow should be created successfully
    And the workflow should process messages

  Scenario: Create dead letter queue workflow
    When I create a workflow with:
      | name        | Dead Letter Queue Workflow |
      | description | Handle failed message processing |
      | config      | modules:\n  - name: main-broker\n    type: messaging.broker\n  - name: primary-processor\n    type: messaging.handler\n  - name: retry-processor\n    type: messaging.handler\n  - name: dlq-processor\n    type: messaging.handler\n\nworkflows:\n  messaging:\n    subscriptions:\n      - topic: main-queue\n        handler: primary-processor\n      - topic: retry-queue\n        handler: retry-processor\n      - topic: dead-letter-queue\n        handler: dlq-processor\n    producers:\n      - name: primary-processor\n        forwardTo:\n          - retry-queue\n          - dead-letter-queue\n      - name: retry-processor\n        forwardTo:\n          - dead-letter-queue |
    Then the workflow should be created successfully
    And the workflow should process messages