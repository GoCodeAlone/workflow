Feature: Modular Library Module Testing
  As a workflow developer
  I want to test the GoCodeAlone modular library modules
  So that I can leverage the full ecosystem of modular components

  Background:
    Given the workflow UI is running
    And I am logged in as an admin user

  Scenario: Create workflow with modular HTTP server
    When I create a modular workflow with:
      | module | httpserver.modular |
    Then the workflow should be created successfully
    And the workflow should use "httpserver.modular" module
    And the workflow should include modular components

  Scenario: Create workflow with modular scheduler
    When I create a modular workflow with:
      | module | scheduler.modular |
    Then the workflow should be created successfully
    And the workflow should use "scheduler.modular" module
    And the workflow should include modular components

  Scenario: Create workflow with modular authentication
    When I create a modular workflow with:
      | module | auth.modular |
    Then the workflow should be created successfully
    And the workflow should use "auth.modular" module
    And the workflow should include modular components

  Scenario: Create workflow with modular event bus
    When I create a modular workflow with:
      | module | eventbus.modular |
    Then the workflow should be created successfully
    And the workflow should use "eventbus.modular" module
    And the workflow should include modular components

  Scenario: Create workflow with modular cache
    When I create a modular workflow with:
      | module | cache.modular |
    Then the workflow should be created successfully
    And the workflow should use "cache.modular" module
    And the workflow should include modular components

  Scenario: Create workflow with Chi router
    When I create a modular workflow with:
      | module | chimux.router |
    Then the workflow should be created successfully
    And the workflow should use "chimux.router" module
    And the workflow should include modular components

  Scenario: Create workflow with modular event logger
    When I create a modular workflow with:
      | module | eventlogger.modular |
    Then the workflow should be created successfully
    And the workflow should use "eventlogger.modular" module
    And the workflow should include modular components

  Scenario: Create workflow with modular HTTP client
    When I create a modular workflow with:
      | module | httpclient.modular |
    Then the workflow should be created successfully
    And the workflow should use "httpclient.modular" module
    And the workflow should include modular components

  Scenario: Create workflow with modular database
    When I create a modular workflow with:
      | module | database.modular |
    Then the workflow should be created successfully
    And the workflow should use "database.modular" module
    And the workflow should include modular components

  Scenario: Create workflow with modular JSON schema
    When I create a modular workflow with:
      | module | jsonschema.modular |
    Then the workflow should be created successfully
    And the workflow should use "jsonschema.modular" module
    And the workflow should include modular components

  Scenario: Create comprehensive modular workflow
    When I create a workflow with:
      | name        | Comprehensive Modular Workflow |
      | description | Workflow using multiple modular components |
      | config_file | comprehensive_modular_workflow.yaml |
    Then the workflow should be created successfully
    And the workflow should include modular components

  Scenario: Create API gateway with modular components
    When I create a workflow with:
      | name        | Modular API Gateway |
      | description | API gateway built with modular components |
      | config_file | modular_api_gateway.yaml |
    Then the workflow should be created successfully
    And the workflow should include modular components

  Scenario: Create data processing workflow with modular components
    When I create a workflow with:
      | name        | Modular Data Processing |
      | description | Data processing using modular components |
      | config_file | modular_data_processing.yaml |
    Then the workflow should be created successfully
    And the workflow should include modular components

  Scenario: Create microservices architecture with modular components
    When I create a workflow with:
      | name        | Modular Microservices Architecture |
      | description | Microservices using modular components |
      | config_file | modular_microservices_architecture.yaml |
    Then the workflow should be created successfully
    And the workflow should include modular components

  Scenario: Create real-time messaging platform with modular components
    When I create a workflow with:
      | name        | Modular Real-time Messaging Platform |
      | description | Real-time messaging using modular components |
      | config_file | modular_realtime_messaging.yaml |
    Then the workflow should be created successfully
    And the workflow should include modular components

  Scenario: Create monitoring and alerting system with modular components
    When I create a workflow with:
      | name        | Modular Monitoring and Alerting |
      | description | Monitoring system using modular components |
      | config_file | modular_monitoring_alerting.yaml |
    Then the workflow should be created successfully
    And the workflow should include modular components

  Scenario: Create e-commerce platform with all modular components
    When I create a workflow with:
      | name        | Complete Modular E-commerce Platform |
      | description | Full e-commerce platform using all modular components |
      | config_file | complete_modular_ecommerce.yaml |
    Then the workflow should be created successfully
    And the workflow should include modular components