Feature: Undefined steps — strict mode test
  # In lenient mode (default) the suite passes even though these steps have no definitions.
  # In strict mode (bdd.Strict() option) the suite fails.
  Scenario: This has undefined steps
    Given the workflow engine is loaded with config:
      """yaml
      pipelines:
        test:
          steps:
            - name: s
              type: step.set
              config:
                values: { x: 1 }
      """
    When I do something that has no step definition
    Then it should fail in strict mode
