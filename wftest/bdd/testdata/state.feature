Feature: State step definitions
  Scenario: Seed state from fixture file and assert values
    Given the workflow engine is loaded with config:
      """yaml
      pipelines:
        noop:
          steps:
            - name: pass
              type: step.set
              config:
                values:
                  ok: "true"
      """
    And state "users" is seeded from "testdata/fixture.json"
    When I execute pipeline "noop"
    Then the pipeline should succeed
    And state "users" key "user:1" field "name" should be "Alice"
    And state "users" key "user:2" field "name" should be "Bob"

  Scenario: Seed state with inline table and assert
    Given the workflow engine is loaded with config:
      """yaml
      pipelines:
        noop:
          steps:
            - name: pass
              type: step.set
              config:
                values:
                  ok: "true"
      """
    And state "products" has key "sku:001" with:
      | name  | Widget    |
      | price | 9.99      |
    When I execute pipeline "noop"
    Then the pipeline should succeed
    And state "products" key "sku:001" field "name" should be "Widget"
    And state "products" key "sku:001" field "price" should be "9.99"
