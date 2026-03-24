Feature: HTTP trigger step definitions
  Scenario: GET request returns 200
    Given the workflow engine is loaded with config:
      """yaml
      modules:
        - name: router
          type: http.router
      pipelines:
        hello:
          trigger:
            type: http
            config:
              method: GET
              path: /hello
          steps:
            - name: reply
              type: step.json_response
              config:
                status: 200
                body:
                  message: "hello world"
      """
    When I GET "/hello"
    Then the response status should be 200
    And the response JSON "message" should be "hello world"

  Scenario: POST request with JSON body returns 201
    Given the workflow engine is loaded with config:
      """yaml
      modules:
        - name: router
          type: http.router
      pipelines:
        create:
          trigger:
            type: http
            config:
              method: POST
              path: /items
          steps:
            - name: reply
              type: step.json_response
              config:
                status: 201
                body:
                  created: true
      """
    When I POST "/items" with JSON:
      """json
      {"name": "widget"}
      """
    Then the response status should be 201
    And the response body should contain "created"

  Scenario: PATCH request returns 200
    Given the workflow engine is loaded with config:
      """yaml
      modules:
        - name: router
          type: http.router
      pipelines:
        update_item:
          trigger:
            type: http
            config:
              path: /api/items/123
              method: PATCH
          steps:
            - name: respond
              type: step.json_response
              config:
                status: 200
                body:
                  patched: true
      """
    When I PATCH "/api/items/123" with JSON:
      """json
      {"name": "updated"}
      """
    Then the response status should be 200
    And the response body should contain "patched"

  Scenario: HEAD request returns 200
    Given the workflow engine is loaded with config:
      """yaml
      modules:
        - name: router
          type: http.router
      pipelines:
        check_item:
          trigger:
            type: http
            config:
              path: /api/items/123
              method: HEAD
          steps:
            - name: respond
              type: step.json_response
              config:
                status: 200
      """
    When I HEAD "/api/items/123"
    Then the response status should be 200
