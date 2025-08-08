Feature: Integration Workflow Testing
  As a workflow developer
  I want to create complex workflows that integrate multiple module types
  So that I can build sophisticated systems combining HTTP, messaging, scheduling, and state machines

  Background:
    Given the workflow UI is running
    And I am logged in as an admin user

  Scenario: Create API-driven data pipeline workflow
    When I create a workflow with:
      | name        | API-Driven Data Pipeline |
      | description | HTTP API that triggers data processing pipeline |
      | config_file | api_driven_data_pipeline.yaml |
    Then the workflow should be created successfully
    And the workflow should use "http.server" module
    And the workflow should use "messaging.broker" module
    And the workflow should use "scheduler" module

  Scenario: Create e-commerce order processing workflow
    When I create a workflow with:
      | name        | E-commerce Order Processing |
      | description | Complete order processing with state management |
      | config_file | ecommerce_order_processing.yaml |
    Then the workflow should be created successfully
    And the workflow should manage state transitions
    And the workflow should process messages
    And the workflow should execute on schedule

  Scenario: Create IoT monitoring and alerting workflow
    When I create a workflow with:
      | name        | IoT Monitoring and Alerting |
      | description | IoT device monitoring with real-time alerting |
      | config_file | iot_monitoring_alerting.yaml |
    Then the workflow should be created successfully

  Scenario: Create microservices API gateway workflow
    When I create a workflow with:
      | name        | Microservices API Gateway |
      | description | API gateway with load balancing and circuit breaker |
      | config_file | microservices_api_gateway.yaml |
    Then the workflow should be created successfully

  Scenario: Create real-time chat application workflow
    When I create a workflow with:
      | name        | Real-time Chat Application |
      | description | Chat application with user management and message processing |
      | config_file | realtime_chat_application.yaml |
    Then the workflow should be created successfully

  Scenario: Create monitoring dashboard workflow
    When I create a workflow with:
      | name        | Monitoring Dashboard |
      | description | Real-time monitoring dashboard with alerting |
      | config_file | monitoring_dashboard.yaml |
    Then the workflow should be created successfully