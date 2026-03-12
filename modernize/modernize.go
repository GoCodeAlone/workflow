// Package modernize detects and fixes known YAML config anti-patterns
// in workflow configuration files.
package modernize

import (
	"strings"

	"gopkg.in/yaml.v3"
)

// Finding represents a single issue detected by a modernize rule.
type Finding struct {
	RuleID  string `json:"rule_id"`
	Line    int    `json:"line"`
	Message string `json:"message"`
	Fixable bool   `json:"fixable"`
}

// Change represents a modification applied by a rule's Fix function.
type Change struct {
	RuleID      string `json:"rule_id"`
	Line        int    `json:"line"`
	Description string `json:"description"`
}

// Rule defines a modernize transformation rule.
type Rule struct {
	ID          string
	Description string
	Severity    string // "error" or "warning"
	Check       func(root *yaml.Node, raw []byte) []Finding
	Fix         func(root *yaml.Node) []Change
}

// AllRules returns all registered modernize rules.
func AllRules() []Rule {
	return []Rule{
		hyphenStepsRule(),
		conditionalFieldRule(),
		dbQueryModeRule(),
		dbQueryIndexRule(),
		absoluteDbPathRule(),
		emptyRoutesRule(),
		camelCaseConfigRule(),
		requestParseConfigRule(),
	}
}

// FilterRules filters the rule list based on include/exclude flags.
func FilterRules(rules []Rule, include, exclude string) []Rule {
	if include == "" && exclude == "" {
		return rules
	}

	includeSet := make(map[string]bool)
	if include != "" {
		for _, id := range strings.Split(include, ",") {
			includeSet[strings.TrimSpace(id)] = true
		}
	}

	excludeSet := make(map[string]bool)
	if exclude != "" {
		for _, id := range strings.Split(exclude, ",") {
			excludeSet[strings.TrimSpace(id)] = true
		}
	}

	var filtered []Rule
	for _, r := range rules {
		if len(includeSet) > 0 && !includeSet[r.ID] {
			continue
		}
		if excludeSet[r.ID] {
			continue
		}
		filtered = append(filtered, r)
	}
	return filtered
}
