Feature: Multi-Tenant Workflow Testing
  As a platform administrator
  I want to ensure tenant isolation in workflow management
  So that each tenant can have their own workflows without interference

  Background:
    Given the workflow UI is running
    And there are tenants: "tenant-a,tenant-b,tenant-c"

  Scenario: Tenant isolation for workflow access
    Given there is a user "admin-a" in tenant "tenant-a"
    And there is a user "admin-b" in tenant "tenant-b"
    When I login as "admin-a" in tenant "tenant-a"
    And I create a workflow with:
      | name        | Tenant A Workflow |
      | description | Workflow for tenant A |
      | config      | modules:\n  - name: http-server\n    type: http.server |
    Then the workflow should be created successfully
    When I login as "admin-b" in tenant "tenant-b"
    Then I should only see workflows for my tenant
    And I should not see workflows from other tenants

  Scenario: Multiple workflow types per tenant
    Given I login as "admin" in tenant "tenant-a"
    When I create 2 "HTTP" workflows in tenant "tenant-a"
    And I create 3 "Messaging" workflows in tenant "tenant-a"
    And I create 1 "Scheduler" workflows in tenant "tenant-a"
    Then tenant "tenant-a" should have 6 workflows
    When I switch to tenant "tenant-b"
    And I create 1 "StateMachine" workflows in tenant "tenant-b"
    And I create 2 "HTTP" workflows in tenant "tenant-b"
    Then tenant "tenant-b" should have 3 workflows
    And each tenant should have isolated workflows

  Scenario: Cross-tenant workflow configuration variety
    Given I login as "admin" in tenant "tenant-a"
    When I create an HTTP server workflow with:
      | address | :8080 |
      | route   | GET /tenant-a/api |
    And I create a messaging workflow with:
      | topic   | tenant-a-events |
      | handler | tenant-a-handler |
    Then the workflow should be created successfully
    When I switch to tenant "tenant-b"
    And I create an HTTP server workflow with:
      | address | :8081 |
      | route   | GET /tenant-b/api |
    And I create a scheduler workflow with:
      | cronExpression | 0 * * * * |
      | jobName        | tenant-b-job |
    Then the workflow should be created successfully
    And each tenant should have isolated workflows

  Scenario: Tenant-specific module configurations
    Given I login as "admin" in tenant "tenant-a"
    When I create a workflow with:
      | name        | Tenant A Custom Workflow |
      | description | Custom configuration for tenant A |
      | config      | modules:\n  - name: tenant-a-server\n    type: http.server\n    config:\n      address: ":8080"\n  - name: tenant-a-auth\n    type: http.middleware.auth\n    config:\n      authType: "Bearer"\n  - name: tenant-a-broker\n    type: messaging.broker\n  - name: tenant-a-scheduler\n    type: scheduler\n    config:\n      cronExpression: "0 6 * * *"\n\nworkflows:\n  http:\n    routes:\n      - method: GET\n        path: /tenant-a/secure\n        handler: tenant-a-handler\n        middlewares:\n          - tenant-a-auth\n  scheduler:\n    jobs:\n      - scheduler: tenant-a-scheduler\n        job: tenant-a-daily-job |
    Then the workflow should be created successfully
    When I switch to tenant "tenant-b"
    And I create a workflow with:
      | name        | Tenant B Custom Workflow |
      | description | Different configuration for tenant B |
      | config      | modules:\n  - name: tenant-b-server\n    type: http.server\n    config:\n      address: ":8081"\n  - name: tenant-b-cors\n    type: http.middleware.cors\n    config:\n      allowedOrigins: ["https://tenant-b.example.com"]\n  - name: tenant-b-engine\n    type: statemachine.engine\n  - name: tenant-b-tracker\n    type: state.tracker\n\nworkflows:\n  http:\n    routes:\n      - method: POST\n        path: /tenant-b/api\n        handler: tenant-b-handler\n        middlewares:\n          - tenant-b-cors\n  statemachine:\n    engine: tenant-b-engine\n    definitions:\n      - name: tenant-b-process\n        initialState: "started" |
    Then the workflow should be created successfully
    And I can access my tenant's workflows
    And I cannot access other tenants' workflows

  Scenario: Tenant workflow scalability testing
    Given I login as "admin" in tenant "tenant-a"
    When I create 5 "HTTP" workflows in tenant "tenant-a"
    And I create 5 "Messaging" workflows in tenant "tenant-a"
    And I create 5 "Scheduler" workflows in tenant "tenant-a"
    And I create 5 "StateMachine" workflows in tenant "tenant-a"
    Then tenant "tenant-a" should have 20 workflows
    When I switch to tenant "tenant-b"
    And I create 3 "HTTP" workflows in tenant "tenant-b"
    And I create 7 "Messaging" workflows in tenant "tenant-b"
    Then tenant "tenant-b" should have 10 workflows
    When I switch to tenant "tenant-c"
    And I create 2 "Scheduler" workflows in tenant "tenant-c"
    And I create 8 "StateMachine" workflows in tenant "tenant-c"
    Then tenant "tenant-c" should have 10 workflows
    And each tenant should have isolated workflows

  Scenario: Complex multi-module workflows per tenant
    Given I login as "admin" in tenant "tenant-a"
    When I create a workflow with:
      | name        | Tenant A E-commerce Platform |
      | description | Full e-commerce platform for tenant A |
      | config      | modules:\n  - name: web-server\n    type: http.server\n    config:\n      address: ":8080"\n  - name: api-router\n    type: http.router\n  - name: auth-middleware\n    type: http.middleware.auth\n  - name: rate-limiter\n    type: http.middleware.ratelimit\n    config:\n      requestsPerMinute: 1000\n  - name: users-api\n    type: api.handler\n    config:\n      resourceName: "users"\n  - name: orders-api\n    type: api.handler\n    config:\n      resourceName: "orders"\n  - name: event-broker\n    type: messaging.broker\n  - name: order-processor\n    type: messaging.handler\n  - name: notification-service\n    type: messaging.handler\n  - name: daily-reports\n    type: scheduler\n    config:\n      cronExpression: "0 6 * * *"\n  - name: report-generator\n    type: messaging.handler\n  - name: order-state-machine\n    type: statemachine.engine\n  - name: order-tracker\n    type: state.tracker\n\nworkflows:\n  http:\n    routes:\n      - method: GET\n        path: /api/users\n        handler: users-api\n        middlewares: [auth-middleware, rate-limiter]\n      - method: POST\n        path: /api/orders\n        handler: orders-api\n        middlewares: [auth-middleware]\n  messaging:\n    subscriptions:\n      - topic: order.created\n        handler: order-processor\n      - topic: order.processed\n        handler: notification-service\n  scheduler:\n    jobs:\n      - scheduler: daily-reports\n        job: report-generator\n  statemachine:\n    engine: order-state-machine\n    definitions:\n      - name: order-lifecycle\n        initialState: "created" |
    Then the workflow should be created successfully
    When I switch to tenant "tenant-b"
    And I create a workflow with:
      | name        | Tenant B Analytics Platform |
      | description | Data analytics platform for tenant B |
      | config      | modules:\n  - name: analytics-server\n    type: http.server\n    config:\n      address: ":8081"\n  - name: analytics-router\n    type: http.router\n  - name: cors-middleware\n    type: http.middleware.cors\n  - name: analytics-api\n    type: api.handler\n    config:\n      resourceName: "analytics"\n  - name: data-broker\n    type: messaging.broker\n  - name: data-collector\n    type: messaging.handler\n  - name: data-processor\n    type: messaging.handler\n  - name: data-aggregator\n    type: messaging.handler\n  - name: hourly-processor\n    type: scheduler\n    config:\n      cronExpression: "0 * * * *"\n  - name: processing-job\n    type: messaging.handler\n\nworkflows:\n  http:\n    routes:\n      - method: POST\n        path: /api/data\n        handler: analytics-api\n        middlewares: [cors-middleware]\n  messaging:\n    subscriptions:\n      - topic: raw-data\n        handler: data-collector\n      - topic: collected-data\n        handler: data-processor\n      - topic: processed-data\n        handler: data-aggregator\n    producers:\n      - name: data-collector\n        forwardTo: [collected-data]\n      - name: data-processor\n        forwardTo: [processed-data]\n  scheduler:\n    jobs:\n      - scheduler: hourly-processor\n        job: processing-job |
    Then the workflow should be created successfully
    And each tenant should have isolated workflows

  Scenario: Tenant authentication and authorization
    Given I login as "admin" in tenant "tenant-a"
    And I create a workflow with:
      | name        | Tenant A Secure Workflow |
      | description | Secure workflow for tenant A |
      | config      | modules:\n  - name: secure-server\n    type: http.server |
    Then the workflow should be created successfully
    When I login as "admin" in tenant "tenant-b"
    Then I should only see workflows for my tenant
    And I should receive a successful response
    When I try to access "/api/tenant-a-workflows" without a token
    Then I should receive an unauthorized error