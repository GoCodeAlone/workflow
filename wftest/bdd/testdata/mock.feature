Feature: Mock support
  Scenario: Mock step with JSON docstring
    Given the workflow engine is loaded with config:
      """yaml
      pipelines:
        query:
          steps:
            - name: fetch
              type: step.db_query
              config:
                database: db
                query: "SELECT 1"
                mode: single
      """
    And step "step.db_query" returns JSON:
      """json
      {"row": {"id": 1, "name": "test"}, "found": true}
      """
    When I execute pipeline "query"
    Then the pipeline should succeed

  Scenario: Mock step with table
    Given the workflow engine is loaded with config:
      """yaml
      pipelines:
        lookup:
          steps:
            - name: find
              type: step.custom
              config: {}
      """
    And step "step.custom" is mocked to return:
      | status | ok |
      | count  | 42 |
    When I execute pipeline "lookup"
    Then the pipeline should succeed
    And the pipeline output "status" should be "ok"
