package modernize

import (
	"testing"
)

func TestRequestParseConfigRule_ParseHeadersBool(t *testing.T) {
	input := `
pipelines:
  test:
    steps:
      - name: parse_req
        type: step.request_parse
        config:
          parse_headers: true
`
	root := parseYAML(t, input)
	rule := requestParseConfigRule()
	findings := rule.Check(root, []byte(input))

	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	f := findings[0]
	if f.RuleID != "request-parse-config" {
		t.Errorf("expected rule ID request-parse-config, got %s", f.RuleID)
	}
	if f.Fixable {
		t.Error("expected Fixable=false")
	}
	if f.Message == "" {
		t.Error("expected non-empty message")
	}
}

func TestRequestParseConfigRule_ParseBodyTrue(t *testing.T) {
	input := `
pipelines:
  test:
    steps:
      - name: parse_req
        type: step.request_parse
        config:
          parse_body: true
`
	root := parseYAML(t, input)
	rule := requestParseConfigRule()
	findings := rule.Check(root, []byte(input))

	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].Message == "" {
		t.Error("expected non-empty message")
	}
}

func TestRequestParseConfigRule_BothIssues(t *testing.T) {
	input := `
pipelines:
  test:
    steps:
      - name: parse_req
        type: step.request_parse
        config:
          parse_headers: true
          parse_body: true
`
	root := parseYAML(t, input)
	rule := requestParseConfigRule()
	findings := rule.Check(root, []byte(input))

	if len(findings) != 2 {
		t.Fatalf("expected 2 findings, got %d", len(findings))
	}
}

func TestRequestParseConfigRule_ValidArray(t *testing.T) {
	input := `
pipelines:
  test:
    steps:
      - name: parse_req
        type: step.request_parse
        config:
          parse_headers:
            - Authorization
            - Content-Type
`
	root := parseYAML(t, input)
	rule := requestParseConfigRule()
	findings := rule.Check(root, []byte(input))

	if len(findings) != 0 {
		t.Fatalf("expected 0 findings for array parse_headers, got %d", len(findings))
	}
}

func TestRequestParseConfigRule_ValidNewKeys(t *testing.T) {
	input := `
pipelines:
  test:
    steps:
      - name: parse_req
        type: step.request_parse
        config:
          headers:
            - Authorization
          body_fields:
            - name
            - email
`
	root := parseYAML(t, input)
	rule := requestParseConfigRule()
	findings := rule.Check(root, []byte(input))

	if len(findings) != 0 {
		t.Fatalf("expected 0 findings for valid new-style keys, got %d", len(findings))
	}
}

func TestRequestParseConfigRule_EmptyConfig(t *testing.T) {
	input := `
pipelines:
  test:
    steps:
      - name: parse_req
        type: step.request_parse
        config: {}
`
	root := parseYAML(t, input)
	rule := requestParseConfigRule()
	findings := rule.Check(root, []byte(input))

	if len(findings) != 0 {
		t.Fatalf("expected 0 findings for empty config, got %d", len(findings))
	}
}

func TestRequestParseConfigRule_NoConfig(t *testing.T) {
	input := `
pipelines:
  test:
    steps:
      - name: parse_req
        type: step.request_parse
`
	root := parseYAML(t, input)
	rule := requestParseConfigRule()
	findings := rule.Check(root, []byte(input))

	if len(findings) != 0 {
		t.Fatalf("expected 0 findings for missing config, got %d", len(findings))
	}
}

func TestRequestParseConfigRule_ParseBodyFalse(t *testing.T) {
	input := `
pipelines:
  test:
    steps:
      - name: parse_req
        type: step.request_parse
        config:
          parse_body: false
`
	root := parseYAML(t, input)
	rule := requestParseConfigRule()
	findings := rule.Check(root, []byte(input))

	// parse_body: false is not flagged (only true is unnecessary)
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings for parse_body: false, got %d", len(findings))
	}
}

func TestRequestParseConfigRule_ParseHeadersFalse(t *testing.T) {
	input := `
pipelines:
  test:
    steps:
      - name: parse_req
        type: step.request_parse
        config:
          parse_headers: false
`
	root := parseYAML(t, input)
	rule := requestParseConfigRule()
	findings := rule.Check(root, []byte(input))

	// parse_headers: false (boolean) should still warn — should be an array or removed
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding for parse_headers: false (boolean), got %d", len(findings))
	}
}

func TestRequestParseConfigRule_NotFixable(t *testing.T) {
	rule := requestParseConfigRule()
	if rule.Fix != nil {
		t.Error("expected Fix to be nil for detect-only rule")
	}
}

func TestRequestParseConfigRule_RegisteredInAllRules(t *testing.T) {
	rules := AllRules()
	found := false
	for _, r := range rules {
		if r.ID == "request-parse-config" {
			found = true
			if r.Severity != "warning" {
				t.Errorf("expected severity warning, got %s", r.Severity)
			}
			break
		}
	}
	if !found {
		t.Error("request-parse-config rule not found in AllRules()")
	}
}
