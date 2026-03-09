package modernize

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func parseYAML(t *testing.T, input string) *yaml.Node {
	t.Helper()
	var root yaml.Node
	if err := yaml.Unmarshal([]byte(input), &root); err != nil {
		t.Fatalf("failed to parse YAML: %v", err)
	}
	return &root
}

func TestHyphenSteps_DoesNotCorruptUnrelatedValues(t *testing.T) {
	// The step name is "check-xss" but a config value contains "check-xss" as a
	// substring in a URL or description. The fix must NOT alter those unrelated values.
	input := `
pipelines:
  security:
    steps:
      - name: check-xss
        type: step.set
        config:
          values:
            status: "running check-xss-scanner v2"
            url: "https://example.com/check-xss-endpoint"
        next: log-result
      - name: log-result
        type: step.set
        config:
          values:
            message: "done"
`
	root := parseYAML(t, input)
	rule := hyphenStepsRule()
	changes := rule.Fix(root)

	out, err := yaml.Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	output := string(out)

	// Step name should be renamed
	if !strings.Contains(output, "name: check_xss") {
		t.Error("expected step name to be renamed to check_xss")
	}

	// next reference should be renamed
	if !strings.Contains(output, "next: log_result") {
		t.Error("expected next reference to be renamed to log_result")
	}

	// Unrelated values must NOT be corrupted
	if !strings.Contains(output, "running check-xss-scanner v2") {
		t.Errorf("unrelated config value was corrupted: status field should still contain 'running check-xss-scanner v2'\noutput:\n%s", output)
	}
	if !strings.Contains(output, "https://example.com/check-xss-endpoint") {
		t.Errorf("unrelated config value was corrupted: url field should still contain original URL\noutput:\n%s", output)
	}

	if len(changes) == 0 {
		t.Error("expected at least one change")
	}
	t.Logf("changes: %d", len(changes))
	for _, c := range changes {
		t.Logf("  %s (line %d): %s", c.RuleID, c.Line, c.Description)
	}
}

func TestHyphenSteps_RenamesTemplateIndexExpressions(t *testing.T) {
	input := `
pipelines:
  main:
    steps:
      - name: parse-request
        type: step.request_parse
        config: {}
      - name: use-data
        type: step.set
        config:
          values:
            result: '{{ index .steps "parse-request" "body" }}'
`
	root := parseYAML(t, input)
	rule := hyphenStepsRule()
	rule.Fix(root)

	out, err := yaml.Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	output := string(out)

	if !strings.Contains(output, `index .steps "parse_request" "body"`) {
		t.Errorf("template index expression not updated\noutput:\n%s", output)
	}
	if !strings.Contains(output, "name: parse_request") {
		t.Error("step name not renamed")
	}
}

func TestHyphenSteps_RenamesConditionalFieldPaths(t *testing.T) {
	input := `
pipelines:
  main:
    steps:
      - name: check-status
        type: step.set
        config:
          values:
            matched: "true"
      - name: branch
        type: step.conditional
        config:
          field: steps.check-status.matched
          routes:
            "true": handle-true
          default: handle-false
      - name: handle-true
        type: step.set
        config:
          values:
            msg: ok
      - name: handle-false
        type: step.set
        config:
          values:
            msg: fail
`
	root := parseYAML(t, input)
	rule := hyphenStepsRule()
	rule.Fix(root)

	out, err := yaml.Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	output := string(out)

	if !strings.Contains(output, "steps.check_status.matched") {
		t.Errorf("conditional field path not updated\noutput:\n%s", output)
	}
	if !strings.Contains(output, "name: check_status") {
		t.Error("step name not renamed")
	}
	// Route values and default that are exact step name matches
	if !strings.Contains(output, "handle_true") {
		t.Errorf("route value not updated\noutput:\n%s", output)
	}
	if !strings.Contains(output, "handle_false") {
		t.Errorf("default value not updated\noutput:\n%s", output)
	}
}

func TestHyphenSteps_Check(t *testing.T) {
	input := `
pipelines:
  main:
    steps:
      - name: my-step
        type: step.set
        config: {}
      - name: safe_step
        type: step.set
        config: {}
`
	root := parseYAML(t, input)
	rule := hyphenStepsRule()
	findings := rule.Check(root, []byte(input))

	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].Message == "" {
		t.Error("finding should have a message")
	}
	if !findings[0].Fixable {
		t.Error("finding should be fixable")
	}
}
