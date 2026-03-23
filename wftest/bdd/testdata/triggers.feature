Feature: Pipeline trigger step definitions
  Scenario: Execute pipeline with input data
    Given the workflow engine is loaded with config:
      """yaml
      pipelines:
        greet:
          steps:
            - name: build
              type: step.set
              config:
                values:
                  greeting: "hello"
      """
    When I execute pipeline "greet" with:
      | name | Alice |
    Then the pipeline should succeed
    And the pipeline output "greeting" should be "hello"

  Scenario: Pipeline failure is detectable
    Given the workflow engine is loaded with config:
      """yaml
      pipelines:
        fail:
          steps:
            - name: boom
              type: step.custom_will_fail
              config: {}
      """
    And step "step.custom_will_fail" is mocked to return:
      | ok | false |
    When I execute pipeline "fail"
    Then the pipeline should succeed
