Feature: Assertion step definitions
  Scenario: Assert pipeline output value
    Given the workflow engine is loaded with config:
      """yaml
      pipelines:
        compute:
          steps:
            - name: init
              type: step.set
              config:
                values:
                  result: "42"
                  status: "done"
      """
    When I execute pipeline "compute"
    Then the pipeline should succeed
    And the pipeline output "result" should be "42"
    And the pipeline output "status" should be "done"

  Scenario: Assert step was executed
    Given the workflow engine is loaded with config:
      """yaml
      pipelines:
        multi:
          steps:
            - name: first
              type: step.set
              config:
                values:
                  a: "1"
            - name: second
              type: step.set
              config:
                values:
                  b: "2"
      """
    When I execute pipeline "multi"
    Then the pipeline should succeed
    And step "first" should have been executed
    And step "second" should have been executed

  Scenario: Assert step output value
    Given the workflow engine is loaded with config:
      """yaml
      pipelines:
        score:
          steps:
            - name: calc
              type: step.custom_score
              config: {}
      """
    And step "step.custom_score" is mocked to return:
      | score | 99 |
    When I execute pipeline "score"
    Then the pipeline should succeed
    And step "calc" output "score" should be "99"
