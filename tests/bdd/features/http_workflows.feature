Feature: HTTP Workflow Testing
  As a workflow developer
  I want to create and test HTTP-based workflows
  So that I can build web services and APIs using the workflow engine

  Background:
    Given the workflow UI is running
    And I am logged in as an admin user

  Scenario: Create a basic HTTP server workflow
    When I create an HTTP server workflow with:
      | address | :8080 |
      | route   | GET /api/test |
    Then the workflow should be created successfully
    And the workflow should use "http.server" module
    And the workflow should handle HTTP requests

  Scenario: Create HTTP workflow with multiple routes
    When I create an HTTP server workflow with:
      | address | :8081 |
      | route   | GET /api/users |
      | route   | POST /api/users |
      | route   | GET /api/health |
    Then the workflow should be created successfully
    And the workflow should use "http.router" module
    And the workflow should handle HTTP requests

  Scenario: Create HTTP workflow with middleware
    When I create a workflow with:
      | name        | HTTP Middleware Workflow |
      | description | HTTP server with auth middleware |
      | config_file | http_middleware_workflow.yaml |
    Then the workflow should be created successfully
    And the workflow should use "http.middleware.auth" module

  Scenario: Create HTTP workflow with CORS middleware
    When I create a workflow with:
      | name        | CORS HTTP Workflow |
      | description | HTTP server with CORS support |
      | config_file | cors_http_workflow.yaml |
    Then the workflow should be created successfully
    And the workflow should use "http.middleware.cors" module

  Scenario: Create HTTP workflow with rate limiting
    When I create a workflow with:
      | name        | Rate Limited HTTP Workflow |
      | description | HTTP server with rate limiting |
      | config      | modules:\n  - name: http-server\n    type: http.server\n    config:\n      address: ":8084"\n  - name: rate-limit-middleware\n    type: http.middleware.ratelimit\n    config:\n      requestsPerMinute: 100\n      burstSize: 20\n  - name: api-router\n    type: http.router\n  - name: api-handler\n    type: http.handler\n\nworkflows:\n  http:\n    routes:\n      - method: GET\n        path: /api/limited\n        handler: api-handler\n        middlewares:\n          - rate-limit-middleware |
    Then the workflow should be created successfully
    And the workflow should use "http.middleware.ratelimit" module

  Scenario: Create REST API workflow
    When I create a workflow with:
      | name        | REST API Workflow |
      | description | Full REST API with CRUD operations |
      | config      | modules:\n  - name: http-server\n    type: http.server\n    config:\n      address: ":8085"\n  - name: api-router\n    type: http.router\n  - name: users-api\n    type: api.handler\n    config:\n      resourceName: "users"\n  - name: products-api\n    type: api.handler\n    config:\n      resourceName: "products"\n\nworkflows:\n  http:\n    routes:\n      - method: GET\n        path: /api/users\n        handler: users-api\n      - method: POST\n        path: /api/users\n        handler: users-api\n      - method: GET\n        path: /api/users/{id}\n        handler: users-api\n      - method: PUT\n        path: /api/users/{id}\n        handler: users-api\n      - method: DELETE\n        path: /api/users/{id}\n        handler: users-api\n      - method: GET\n        path: /api/products\n        handler: products-api\n      - method: POST\n        path: /api/products\n        handler: products-api |
    Then the workflow should be created successfully
    And the workflow should use "api.handler" module

  Scenario: Create reverse proxy workflow
    When I create a workflow with:
      | name        | Reverse Proxy Workflow |
      | description | HTTP reverse proxy setup |
      | config      | modules:\n  - name: http-server\n    type: http.server\n    config:\n      address: ":8086"\n  - name: reverse-proxy\n    type: http.proxy\n  - name: api-router\n    type: http.router\n\nworkflows:\n  http:\n    routes:\n      - method: GET\n        path: /proxy/*\n        handler: reverse-proxy |
    Then the workflow should be created successfully
    And the workflow should use "http.proxy" module