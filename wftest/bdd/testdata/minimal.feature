Feature: Minimal BDD test
  Scenario: Execute a simple pipeline
    Given the workflow engine is loaded with config:
      """yaml
      pipelines:
        greet:
          steps:
            - name: hello
              type: step.set
              config:
                values:
                  message: "world"
      """
    When I execute pipeline "greet"
    Then the pipeline should succeed
    And the pipeline output "message" should be "world"
