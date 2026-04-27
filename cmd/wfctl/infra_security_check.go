package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/GoCodeAlone/workflow/interfaces"
	"gopkg.in/yaml.v3"
)

// SecuritySeverity classifies a finding.
type SecuritySeverity string

const (
	SeverityFail SecuritySeverity = "FAIL"
	SeverityWarn SecuritySeverity = "WARN"
)

// SecurityFinding is a single result from a security rule.
type SecurityFinding struct {
	RuleID   string
	Severity SecuritySeverity
	Resource string
	Type     string
	Message  string
}

// SecurityCheckOpts holds flags for a security-check run.
type SecurityCheckOpts struct {
	Strict        bool
	StrictCIDR    bool
	MaxChanges    int
	ExtraRulesDir string
}

// securityRule is a function that evaluates the full plan and returns findings.
type securityRule func(plan interfaces.IaCPlan, opts SecurityCheckOpts) []SecurityFinding

// builtinRules is the ordered list of built-in security rules.
var builtinRules []securityRule

// registerRule appends a rule to the built-in registry (called from infra_security_rules.go).
func registerRule(r securityRule) {
	builtinRules = append(builtinRules, r)
}

// RunSecurityCheck applies all built-in and declarative rules to the plan.
func RunSecurityCheck(plan interfaces.IaCPlan, opts SecurityCheckOpts) []SecurityFinding {
	var all []SecurityFinding
	for _, r := range builtinRules {
		all = append(all, r(plan, opts)...)
	}
	if opts.ExtraRulesDir != "" {
		all = append(all, runDeclarativeRules(plan, opts)...)
	}
	return all
}

// runInfraSecurityCheck is the wfctl infra security-check handler.
func runInfraSecurityCheck(args []string) error {
	fs := flag.NewFlagSet("infra security-check", flag.ContinueOnError)
	var planFile string
	fs.StringVar(&planFile, "plan", "", "Path to plan.json (required)")
	var strict bool
	fs.BoolVar(&strict, "strict", false, "Treat WARN findings as FAIL")
	var strictCIDR bool
	fs.BoolVar(&strictCIDR, "strict-cidr", false, "Treat R7 CIDR-widening warnings as FAIL")
	var maxChanges int
	fs.IntVar(&maxChanges, "max-changes", 20, "Maximum allowed plan actions before R10 fires")
	var rulesDir string
	fs.StringVar(&rulesDir, "rules", "", "Directory of declarative YAML rule files")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if planFile == "" {
		return fmt.Errorf("--plan is required")
	}

	data, err := os.ReadFile(planFile) //nolint:gosec // path comes from CLI flag, not HTTP
	if err != nil {
		return fmt.Errorf("read plan: %w", err)
	}
	var plan interfaces.IaCPlan
	if err := json.Unmarshal(data, &plan); err != nil {
		return fmt.Errorf("parse plan: %w", err)
	}

	opts := SecurityCheckOpts{
		Strict:        strict,
		StrictCIDR:    strictCIDR,
		MaxChanges:    maxChanges,
		ExtraRulesDir: rulesDir,
	}

	findings := RunSecurityCheck(plan, opts)
	if err := renderSecurityFindings(os.Stdout, findings); err != nil {
		log.Printf("security-check render: %v (ignored)", err)
	}

	em := detectCIProvider()
	if sumErr := writeSecurityCheckSummary(em, plan, findings); sumErr != nil {
		log.Printf("security-check step summary: %v (ignored)", sumErr)
	}

	var failCount, warnCount int
	for _, f := range findings {
		switch f.Severity {
		case SeverityFail:
			failCount++
		case SeverityWarn:
			warnCount++
		}
	}
	strictWarnCount := 0
	if strict {
		strictWarnCount = warnCount
	}
	if failCount+strictWarnCount > 0 {
		if strictWarnCount > 0 {
			return fmt.Errorf("security-check: %d FAIL + %d WARN (treated as FAIL via --strict) finding(s) in plan %q", failCount, strictWarnCount, plan.ID)
		}
		return fmt.Errorf("security-check: %d FAIL finding(s) in plan %q", failCount, plan.ID)
	}
	return nil
}

// renderSecurityFindings prints findings as a markdown table to w.
func renderSecurityFindings(w io.Writer, findings []SecurityFinding) error {
	if len(findings) == 0 {
		_, err := fmt.Fprintln(w, "Security check passed — no findings.")
		return err
	}
	if _, err := fmt.Fprintln(w, "## Security Check Findings"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "| Rule | Severity | Resource | Message |"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "|------|----------|----------|---------|"); err != nil {
		return err
	}
	for _, f := range findings {
		// Escape pipe characters to prevent breaking the markdown table.
		resType := strings.ReplaceAll(f.Type, "|", "\\|")
		msg := strings.ReplaceAll(f.Message, "|", "\\|")
		if _, err := fmt.Fprintf(w, "| %s | %s | `%s` (%s) | %s |\n",
			f.RuleID, f.Severity, f.Resource, resType, msg); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(w)
	return err
}

// writeSecurityCheckSummary writes markdown findings to the GHA step summary path.
func writeSecurityCheckSummary(em CIGroupEmitter, plan interfaces.IaCPlan, findings []SecurityFinding) (retErr error) {
	path := em.SummaryPath()
	if path == "" {
		return nil
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600) //nolint:gosec
	if err != nil {
		return fmt.Errorf("open summary: %w", err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && retErr == nil {
			retErr = fmt.Errorf("close summary: %w", cerr)
		}
	}()
	return renderSecurityFindings(f, findings)
}

// ─── Declarative YAML rules ──────────────────────────────────────────────────

// declarativeRule is a simple YAML-defined security rule.
type declarativeRule struct {
	ID                    string `yaml:"id"`
	Description           string `yaml:"description"`
	AppliesToAction       string `yaml:"applies_to_action"`        // comma-separated: create,update
	AppliesToResourceType string `yaml:"applies_to_resource_type"` // e.g. infra.database
	Match                 string `yaml:"match"`                    // e.g. trusted_sources == "0.0.0.0/0"
	Severity              string `yaml:"severity"`
	Message               string `yaml:"message"`
}

func runDeclarativeRules(plan interfaces.IaCPlan, opts SecurityCheckOpts) []SecurityFinding {
	entries, err := os.ReadDir(opts.ExtraRulesDir)
	if err != nil {
		// Fail-loud: a security tool must not silently skip custom rules when the
		// directory is unreadable — return a synthetic FAIL finding so the gate fails.
		return []SecurityFinding{{
			RuleID:   "RULES_DIR_UNREADABLE",
			Severity: SeverityFail,
			Resource: opts.ExtraRulesDir,
			Message:  fmt.Sprintf("failed to read declarative rules directory: %v", err),
		}}
	}
	var rules []declarativeRule
	var loadFindings []SecurityFinding
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		path := filepath.Join(opts.ExtraRulesDir, entry.Name())
		data, err := os.ReadFile(path) //nolint:gosec
		if err != nil {
			loadFindings = append(loadFindings, SecurityFinding{
				RuleID:   "RULE_FILE_UNREADABLE",
				Severity: SeverityFail,
				Resource: entry.Name(),
				Message:  fmt.Sprintf("failed to read rule file: %v", err),
			})
			continue
		}
		var r declarativeRule
		if err := yaml.Unmarshal(data, &r); err != nil {
			loadFindings = append(loadFindings, SecurityFinding{
				RuleID:   "RULE_FILE_INVALID",
				Severity: SeverityFail,
				Resource: entry.Name(),
				Message:  fmt.Sprintf("failed to parse rule file: %v", err),
			})
			continue
		}
		rules = append(rules, r)
	}
	sort.Slice(rules, func(i, j int) bool { return rules[i].ID < rules[j].ID })

	var findings []SecurityFinding
	for _, rule := range rules {
		sev := SecuritySeverity(strings.ToUpper(rule.Severity))
		if sev != SeverityFail && sev != SeverityWarn {
			sev = SeverityFail
		}
		actions := secSplitCSV(rule.AppliesToAction)
		for i := range plan.Actions {
			if rule.AppliesToResourceType != "" && plan.Actions[i].Resource.Type != rule.AppliesToResourceType {
				continue
			}
			if len(actions) > 0 && !secContainsStr(actions, plan.Actions[i].Action) {
				continue
			}
			if evalDeclarativeMatch(rule.Match, plan.Actions[i].Resource.Config) {
				msg := rule.Message
				if msg == "" {
					msg = rule.Description
				}
				findings = append(findings, SecurityFinding{
					RuleID:   rule.ID,
					Severity: sev,
					Resource: plan.Actions[i].Resource.Name,
					Type:     plan.Actions[i].Resource.Type,
					Message:  msg,
				})
			}
		}
	}
	return append(loadFindings, findings...)
}

// evalDeclarativeMatch evaluates a simple match expression against a config map.
// Supported forms:
//   - key == "value"   (string equality)
//   - key != "value"   (string inequality)
//   - key absent       (key not present)
//   - key present      (key present)
func evalDeclarativeMatch(expr string, cfg map[string]any) bool {
	if expr == "" {
		return true
	}
	expr = strings.TrimSpace(expr)
	if idx := strings.Index(expr, " == "); idx >= 0 {
		key := strings.TrimSpace(expr[:idx])
		val := strings.Trim(strings.TrimSpace(expr[idx+4:]), `"`)
		v, _ := cfg[key].(string)
		return v == val
	}
	if idx := strings.Index(expr, " != "); idx >= 0 {
		key := strings.TrimSpace(expr[:idx])
		val := strings.Trim(strings.TrimSpace(expr[idx+4:]), `"`)
		// Key must be present for != to fire; absent key is not a mismatch.
		raw, ok := cfg[key]
		if !ok {
			return false
		}
		v, _ := raw.(string)
		return v != val
	}
	if strings.HasSuffix(expr, " absent") {
		key := strings.TrimSpace(strings.TrimSuffix(expr, " absent"))
		_, ok := cfg[key]
		return !ok
	}
	if strings.HasSuffix(expr, " present") {
		key := strings.TrimSpace(strings.TrimSuffix(expr, " present"))
		_, ok := cfg[key]
		return ok
	}
	return false
}

func secSplitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func secContainsStr(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}
