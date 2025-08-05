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
      | config      | modules:\n  - name: http-server-modular\n    type: httpserver.modular\n  - name: auth-modular\n    type: auth.modular\n  - name: cache-modular\n    type: cache.modular\n  - name: eventbus-modular\n    type: eventbus.modular\n  - name: scheduler-modular\n    type: scheduler.modular\n  - name: database-modular\n    type: database.modular\n  - name: httpclient-modular\n    type: httpclient.modular |
    Then the workflow should be created successfully
    And the workflow should include modular components

  Scenario: Create API gateway with modular components
    When I create a workflow with:
      | name        | Modular API Gateway |
      | description | API gateway built with modular components |
      | config      | modules:\n  - name: gateway-server\n    type: httpserver.modular\n  - name: gateway-router\n    type: chimux.router\n  - name: gateway-auth\n    type: auth.modular\n  - name: gateway-cache\n    type: cache.modular\n  - name: gateway-client\n    type: httpclient.modular\n  - name: gateway-eventbus\n    type: eventbus.modular\n  - name: gateway-logger\n    type: eventlogger.modular |
    Then the workflow should be created successfully
    And the workflow should include modular components

  Scenario: Create data processing workflow with modular components
    When I create a workflow with:
      | name        | Modular Data Processing |
      | description | Data processing using modular components |
      | config      | modules:\n  - name: data-server\n    type: httpserver.modular\n  - name: data-auth\n    type: auth.modular\n  - name: data-cache\n    type: cache.modular\n  - name: data-database\n    type: database.modular\n  - name: data-scheduler\n    type: scheduler.modular\n  - name: data-eventbus\n    type: eventbus.modular\n  - name: data-logger\n    type: eventlogger.modular\n  - name: data-schema\n    type: jsonschema.modular |
    Then the workflow should be created successfully
    And the workflow should include modular components

  Scenario: Create microservices architecture with modular components
    When I create a workflow with:
      | name        | Modular Microservices Architecture |
      | description | Microservices using modular components |
      | config      | modules:\n  - name: user-service-server\n    type: httpserver.modular\n  - name: user-service-auth\n    type: auth.modular\n  - name: user-service-db\n    type: database.modular\n  - name: order-service-server\n    type: httpserver.modular\n  - name: order-service-auth\n    type: auth.modular\n  - name: order-service-db\n    type: database.modular\n  - name: notification-service-server\n    type: httpserver.modular\n  - name: notification-scheduler\n    type: scheduler.modular\n  - name: shared-eventbus\n    type: eventbus.modular\n  - name: shared-cache\n    type: cache.modular\n  - name: api-gateway\n    type: chimux.router\n  - name: central-logger\n    type: eventlogger.modular |
    Then the workflow should be created successfully
    And the workflow should include modular components

  Scenario: Create real-time messaging platform with modular components
    When I create a workflow with:
      | name        | Modular Real-time Messaging Platform |
      | description | Real-time messaging using modular components |
      | config      | modules:\n  - name: messaging-server\n    type: httpserver.modular\n  - name: messaging-router\n    type: chimux.router\n  - name: messaging-auth\n    type: auth.modular\n  - name: messaging-cache\n    type: cache.modular\n  - name: messaging-database\n    type: database.modular\n  - name: messaging-eventbus\n    type: eventbus.modular\n  - name: messaging-logger\n    type: eventlogger.modular\n  - name: messaging-client\n    type: httpclient.modular\n  - name: message-scheduler\n    type: scheduler.modular |
    Then the workflow should be created successfully
    And the workflow should include modular components

  Scenario: Create monitoring and alerting system with modular components
    When I create a workflow with:
      | name        | Modular Monitoring and Alerting |
      | description | Monitoring system using modular components |
      | config      | modules:\n  - name: monitoring-server\n    type: httpserver.modular\n  - name: monitoring-auth\n    type: auth.modular\n  - name: metrics-cache\n    type: cache.modular\n  - name: metrics-database\n    type: database.modular\n  - name: alert-scheduler\n    type: scheduler.modular\n  - name: metrics-eventbus\n    type: eventbus.modular\n  - name: system-logger\n    type: eventlogger.modular\n  - name: external-client\n    type: httpclient.modular\n  - name: alert-schema\n    type: jsonschema.modular |
    Then the workflow should be created successfully
    And the workflow should include modular components

  Scenario: Create e-commerce platform with all modular components
    When I create a workflow with:
      | name        | Complete Modular E-commerce Platform |
      | description | Full e-commerce platform using all modular components |
      | config      | modules:\n  - name: web-server\n    type: httpserver.modular\n  - name: api-router\n    type: chimux.router\n  - name: customer-auth\n    type: auth.modular\n  - name: session-cache\n    type: cache.modular\n  - name: product-database\n    type: database.modular\n  - name: order-scheduler\n    type: scheduler.modular\n  - name: event-bus\n    type: eventbus.modular\n  - name: audit-logger\n    type: eventlogger.modular\n  - name: payment-client\n    type: httpclient.modular\n  - name: order-schema\n    type: jsonschema.modular\n  - name: inventory-server\n    type: httpserver.modular\n  - name: inventory-auth\n    type: auth.modular\n  - name: inventory-cache\n    type: cache.modular\n  - name: inventory-database\n    type: database.modular\n  - name: notification-scheduler\n    type: scheduler.modular |
    Then the workflow should be created successfully
    And the workflow should include modular components