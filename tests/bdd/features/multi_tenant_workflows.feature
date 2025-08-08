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
      | config_file | tenant_a_custom_workflow.yaml |
    Then the workflow should be created successfully
    When I switch to tenant "tenant-b"
    And I create a workflow with:
      | name        | Tenant B Custom Workflow |
      | description | Different configuration for tenant B |
      | config_file | tenant_b_custom_workflow.yaml |
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
      | config_file | tenant_a_ecommerce_platform.yaml |
    Then the workflow should be created successfully
    When I switch to tenant "tenant-b"
    And I create a workflow with:
      | name        | Tenant B Analytics Platform |
      | description | Data analytics platform for tenant B |
      | config_file | tenant_b_analytics_platform.yaml |
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