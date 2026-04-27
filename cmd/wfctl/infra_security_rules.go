package main

import (
	"fmt"
	"net"
	"regexp"
	"sort"
	"strings"

	"github.com/GoCodeAlone/workflow/interfaces"
)

func init() {
	registerRule(ruleR1FirewallOverPermissive)
	registerRule(ruleR2DBWithoutTrustedSources)
	registerRule(ruleR3InternalNamingPubliclyExposed)
	registerRule(ruleR4SecretShapeLiteralInEnvVars)
	registerRule(ruleR5ReplaceOnProtectedResource)
	registerRule(ruleR6DBBackupEncryptionRegression)
	registerRule(ruleR7CIDRWidening)
	registerRule(ruleR8StateBucketPublicACL)
	registerRule(ruleR9RegistryRetentionDisabled)
	registerRule(ruleR10ChangeBlastRadiusCap)
}

// ─── R1: Firewall over-permissive sources ────────────────────────────────────
// infra.firewall create/update where any inbound_rules[*].sources contains
// 0.0.0.0/0 or ::/0 for ports OTHER than 80 and 443.
func ruleR1FirewallOverPermissive(plan interfaces.IaCPlan, _ SecurityCheckOpts) []SecurityFinding {
	var findings []SecurityFinding
	for i := range plan.Actions {
		if plan.Actions[i].Resource.Type != "infra.firewall" {
			continue
		}
		if plan.Actions[i].Action != "create" && plan.Actions[i].Action != "update" {
			continue
		}
		if msgs := r1CheckFirewallConfig(plan.Actions[i].Resource.Config); len(msgs) > 0 {
			for _, msg := range msgs {
				findings = append(findings, SecurityFinding{
					RuleID:   "R1",
					Severity: SeverityFail,
					Resource: plan.Actions[i].Resource.Name,
					Type:     plan.Actions[i].Resource.Type,
					Message:  msg,
				})
			}
		}
	}
	return findings
}

func r1CheckFirewallConfig(cfg map[string]any) []string {
	// Support both "inbound_rules" and "inbound" key variants used across wfctl.
	raw, ok := cfg["inbound_rules"]
	if !ok {
		raw, ok = cfg["inbound"]
		if !ok {
			return nil
		}
	}
	rules := toAnySlice(raw)
	if len(rules) == 0 {
		return nil
	}
	var msgs []string
	publicCIDRs := []string{"0.0.0.0/0", "::/0"}
	allowedPorts := map[string]bool{"80": true, "443": true, "http": true, "https": true}
	for _, ruleRaw := range rules {
		rule, ok := ruleRaw.(map[string]any)
		if !ok {
			continue
		}
		port, _ := rule["ports"].(string)
		if allowedPorts[port] {
			continue
		}
		// Union "sources" and "source" key variants so rules using either key are
		// checked — consistent with R7's r7CollectSources union approach.
		sourceSet := map[string]struct{}{}
		for _, s := range toStringSlice(rule["sources"]) {
			sourceSet[s] = struct{}{}
		}
		for _, s := range toStringSlice(rule["source"]) {
			sourceSet[s] = struct{}{}
		}
		for src := range sourceSet {
			for _, pub := range publicCIDRs {
				if src == pub {
					msgs = append(msgs, fmt.Sprintf("inbound_rules port %q allows %s (R1: non-public ports must not expose to %s)", port, src, pub))
				}
			}
		}
	}
	return msgs
}

// ─── R2: DB without trusted_sources ─────────────────────────────────────────
func ruleR2DBWithoutTrustedSources(plan interfaces.IaCPlan, _ SecurityCheckOpts) []SecurityFinding {
	var findings []SecurityFinding
	for i := range plan.Actions {
		if plan.Actions[i].Resource.Type != "infra.database" {
			continue
		}
		if plan.Actions[i].Action != "create" && plan.Actions[i].Action != "update" {
			continue
		}
		cfg := plan.Actions[i].Resource.Config
		ts, ok := cfg["trusted_sources"]
		if !ok {
			findings = append(findings, SecurityFinding{
				RuleID: "R2", Severity: SeverityFail,
				Resource: plan.Actions[i].Resource.Name, Type: plan.Actions[i].Resource.Type,
				Message: "database has no trusted_sources — access is unrestricted",
			})
			continue
		}
		for _, src := range toStringSlice(ts) {
			if src == "0.0.0.0/0" {
				findings = append(findings, SecurityFinding{
					RuleID: "R2", Severity: SeverityFail,
					Resource: plan.Actions[i].Resource.Name, Type: plan.Actions[i].Resource.Type,
					Message: "database trusted_sources contains 0.0.0.0/0 — access is unrestricted",
				})
				break
			}
		}
	}
	return findings
}

// ─── R3: Internal-naming pattern publicly exposed ────────────────────────────
var r3InternalPattern = regexp.MustCompile(`(^|-)(nats|redis|db|broker|internal)($|-)`)

func ruleR3InternalNamingPubliclyExposed(plan interfaces.IaCPlan, _ SecurityCheckOpts) []SecurityFinding {
	var findings []SecurityFinding
	for i := range plan.Actions {
		if plan.Actions[i].Resource.Type != "infra.container_service" {
			continue
		}
		if plan.Actions[i].Action != "create" && plan.Actions[i].Action != "update" {
			continue
		}
		name := plan.Actions[i].Resource.Name
		if !r3InternalPattern.MatchString(name) {
			continue
		}
		httpPort := plan.Actions[i].Resource.Config["http_port"]
		if httpPort == nil || httpPort == "" || httpPort == 0 {
			continue
		}
		findings = append(findings, SecurityFinding{
			RuleID: "R3", Severity: SeverityFail,
			Resource: name, Type: plan.Actions[i].Resource.Type,
			Message: fmt.Sprintf("container service %q matches internal-naming pattern but has http_port set — internal services should not be publicly exposed", name),
		})
	}
	return findings
}

// ─── R4: Secret-shape literal in env_vars ────────────────────────────────────
var (
	r4StripeLive    = regexp.MustCompile(`sk_live_[A-Za-z0-9]{20,}`)
	r4AWSAccessKey  = regexp.MustCompile(`AKIA[0-9A-Z]{16}`)
	r4JWTBearer     = regexp.MustCompile(`Bearer ey[A-Za-z0-9_-]{20,}`)
	r4GenericSecret = regexp.MustCompile(`^[A-Za-z0-9+/]{40,}={0,2}$`) // base64-ish 40+ chars
	r4SecretKeyName = regexp.MustCompile(`(?i)(key|secret|token|pass|pwd)`)
)

func ruleR4SecretShapeLiteralInEnvVars(plan interfaces.IaCPlan, _ SecurityCheckOpts) []SecurityFinding {
	var findings []SecurityFinding
	for i := range plan.Actions {
		if plan.Actions[i].Action != "create" && plan.Actions[i].Action != "update" {
			continue
		}
		envVarsRaw, ok := plan.Actions[i].Resource.Config["env_vars"]
		if !ok {
			continue
		}
		envVars, ok := envVarsRaw.(map[string]any)
		if !ok {
			continue
		}
		for k, v := range envVars {
			val, ok := v.(string)
			if !ok {
				continue
			}
			// Skip ${...} references — these are injected at runtime, not literals.
			if strings.Contains(val, "${") {
				continue
			}
			var reason string
			switch {
			case r4StripeLive.MatchString(val):
				reason = "Stripe live secret key literal"
			case r4AWSAccessKey.MatchString(val):
				reason = "AWS access key ID literal"
			case r4JWTBearer.MatchString(val):
				reason = "JWT Bearer token literal"
			case r4SecretKeyName.MatchString(k) && r4GenericSecret.MatchString(val):
				reason = fmt.Sprintf("potential secret literal in env var %q", k)
			}
			if reason != "" {
				findings = append(findings, SecurityFinding{
					RuleID: "R4", Severity: SeverityFail,
					Resource: plan.Actions[i].Resource.Name, Type: plan.Actions[i].Resource.Type,
					Message: fmt.Sprintf("env_vars[%q]: %s — use ${VAR} references instead", k, reason),
				})
			}
		}
	}
	return findings
}

// ─── R5: Replace on protected resource ───────────────────────────────────────
func ruleR5ReplaceOnProtectedResource(plan interfaces.IaCPlan, _ SecurityCheckOpts) []SecurityFinding {
	var findings []SecurityFinding
	for i := range plan.Actions {
		if plan.Actions[i].Action != "replace" {
			continue
		}
		protected, _ := plan.Actions[i].Resource.Config["protected"].(bool)
		if !protected {
			continue
		}
		findings = append(findings, SecurityFinding{
			RuleID: "R5", Severity: SeverityFail,
			Resource: plan.Actions[i].Resource.Name, Type: plan.Actions[i].Resource.Type,
			Message: fmt.Sprintf("resource %q is protected but plan includes a replace (destructive recreate) action", plan.Actions[i].Resource.Name),
		})
	}
	return findings
}

// ─── R6: DB backup/encryption regression ─────────────────────────────────────
func ruleR6DBBackupEncryptionRegression(plan interfaces.IaCPlan, _ SecurityCheckOpts) []SecurityFinding {
	var findings []SecurityFinding
	for i := range plan.Actions {
		if plan.Actions[i].Resource.Type != "infra.database" || plan.Actions[i].Action != "update" || plan.Actions[i].Current == nil {
			continue
		}
		currentCfg := plan.Actions[i].Current.AppliedConfig
		desiredCfg := plan.Actions[i].Resource.Config

		// Check backup regression.
		currentBackups, _ := currentCfg["backups"].(string)
		desiredBackups, _ := desiredCfg["backups"].(string)
		if currentBackups == "on" && desiredBackups == "off" {
			findings = append(findings, SecurityFinding{
				RuleID: "R6", Severity: SeverityFail,
				Resource: plan.Actions[i].Resource.Name, Type: plan.Actions[i].Resource.Type,
				Message: "database update disables backups (current: on → desired: off)",
			})
		}

		// Check at-rest encryption regression.
		currentEnc := boolFromAny(currentCfg["at_rest_encryption"])
		desiredEnc := boolFromAny(desiredCfg["at_rest_encryption"])
		if currentEnc && !desiredEnc {
			findings = append(findings, SecurityFinding{
				RuleID: "R6", Severity: SeverityFail,
				Resource: plan.Actions[i].Resource.Name, Type: plan.Actions[i].Resource.Type,
				Message: "database update disables at-rest encryption (current: true → desired: false)",
			})
		}
	}
	return findings
}

// ─── R7: CIDR widening ────────────────────────────────────────────────────────
func ruleR7CIDRWidening(plan interfaces.IaCPlan, opts SecurityCheckOpts) []SecurityFinding {
	var findings []SecurityFinding
	for i := range plan.Actions {
		if plan.Actions[i].Resource.Type != "infra.firewall" || plan.Actions[i].Action != "update" || plan.Actions[i].Current == nil {
			continue
		}
		currentSrcs := r7CollectSources(plan.Actions[i].Current.AppliedConfig)
		desiredSrcs := r7CollectSources(plan.Actions[i].Resource.Config)
		if desc := r7WideningDescription(desiredSrcs, currentSrcs); desc != "" {
			sev := SeverityWarn
			if opts.StrictCIDR {
				sev = SeverityFail
			}
			findings = append(findings, SecurityFinding{
				RuleID: "R7", Severity: sev,
				Resource: plan.Actions[i].Resource.Name, Type: plan.Actions[i].Resource.Type,
				Message: fmt.Sprintf("firewall CIDR widening detected — %s", desc),
			})
		}
	}
	return findings
}

func r7CollectSources(cfg map[string]any) map[string]bool {
	set := map[string]bool{}
	// Support both "inbound_rules" and "inbound" key variants.
	raw, ok := cfg["inbound_rules"]
	if !ok {
		raw, ok = cfg["inbound"]
		if !ok {
			return set
		}
	}
	for _, ruleRaw := range toAnySlice(raw) {
		rule, ok := ruleRaw.(map[string]any)
		if !ok {
			continue
		}
		// Support both "sources" (plural) and "source" (singular) key variants.
		for _, src := range toStringSlice(rule["sources"]) {
			set[src] = true
		}
		for _, src := range toStringSlice(rule["source"]) {
			set[src] = true
		}
	}
	return set
}

// r7isCIDRNarrowing reports whether candidate is a subnet of (contained in)
// at least one CIDR in the current set. If so the candidate is more restrictive.
func r7isCIDRNarrowing(candidate string, current map[string]bool) bool {
	for c := range current {
		if cidrContains(c, candidate) {
			return true
		}
	}
	return false
}

// r7WideningDescription returns a human-readable description of the CIDR
// widening, or "" if no widening is detected.
func r7WideningDescription(desired, current map[string]bool) string {
	var added []string
	var broadened []string
	for d := range desired {
		if current[d] {
			continue
		}
		if r7isCIDRNarrowing(d, current) {
			continue
		}
		// Determine whether d broadens an existing CIDR or is entirely new.
		broadensExisting := false
		for c := range current {
			if cidrContains(d, c) {
				broadened = append(broadened, fmt.Sprintf("%s (broadens %s)", d, c))
				broadensExisting = true
				break
			}
		}
		if !broadensExisting {
			added = append(added, d)
		}
	}
	// Sort for deterministic output (map iteration is random).
	sort.Strings(added)
	sort.Strings(broadened)
	var parts []string
	if len(added) > 0 {
		parts = append(parts, "new sources: "+strings.Join(added, ", "))
	}
	if len(broadened) > 0 {
		parts = append(parts, "broadened prefix: "+strings.Join(broadened, ", "))
	}
	return strings.Join(parts, "; ")
}

// cidrContains reports whether widerStr is a network that contains (is a
// superset of) the network narrowerStr. Returns false for any parse error or
// mismatched address families (e.g. IPv4 vs IPv6).
func cidrContains(widerStr, narrowerStr string) bool {
	_, wNet, err := net.ParseCIDR(widerStr)
	if err != nil {
		return false
	}
	_, nNet, err := net.ParseCIDR(narrowerStr)
	if err != nil {
		return false
	}
	// wider contains narrower iff:
	// 1. wider's prefix length ≤ narrower's (broader or equal range), AND
	// 2. wider's network contains narrower's network address.
	wOnes, _ := wNet.Mask.Size()
	nOnes, _ := nNet.Mask.Size()
	return wOnes <= nOnes && wNet.Contains(nNet.IP)
}

// ─── R8: State bucket public ACL ─────────────────────────────────────────────
func ruleR8StateBucketPublicACL(plan interfaces.IaCPlan, _ SecurityCheckOpts) []SecurityFinding {
	var findings []SecurityFinding
	for i := range plan.Actions {
		if plan.Actions[i].Resource.Type != "infra.storage" {
			continue
		}
		if plan.Actions[i].Action != "create" && plan.Actions[i].Action != "update" {
			continue
		}
		acl, _ := plan.Actions[i].Resource.Config["acl"].(string)
		if acl != "" && acl != "private" {
			findings = append(findings, SecurityFinding{
				RuleID: "R8", Severity: SeverityFail,
				Resource: plan.Actions[i].Resource.Name, Type: plan.Actions[i].Resource.Type,
				Message: fmt.Sprintf("storage bucket acl is %q — must be \"private\" to prevent public data exposure", acl),
			})
		}
	}
	return findings
}

// ─── R9: Registry retention disabled ─────────────────────────────────────────
func ruleR9RegistryRetentionDisabled(plan interfaces.IaCPlan, _ SecurityCheckOpts) []SecurityFinding {
	var findings []SecurityFinding
	for i := range plan.Actions {
		if plan.Actions[i].Resource.Type != "infra.registry" || plan.Actions[i].Action != "update" || plan.Actions[i].Current == nil {
			continue
		}
		currentRetention := r9GetUntaggedTTL(plan.Actions[i].Current.AppliedConfig)
		desiredRetention := r9GetUntaggedTTL(plan.Actions[i].Resource.Config)
		if currentRetention > 0 && desiredRetention == 0 {
			findings = append(findings, SecurityFinding{
				RuleID: "R9", Severity: SeverityWarn,
				Resource: plan.Actions[i].Resource.Name, Type: plan.Actions[i].Resource.Type,
				Message: "registry update removes untagged image retention policy — untagged images will accumulate",
			})
		}
	}
	return findings
}

func r9GetUntaggedTTL(cfg map[string]any) int {
	retRaw, ok := cfg["retention"]
	if !ok {
		return 0
	}
	ret, ok := retRaw.(map[string]any)
	if !ok {
		return 0
	}
	return intFromAny(ret["untagged_ttl"])
}

// ─── R10: Change blast-radius cap ────────────────────────────────────────────
func ruleR10ChangeBlastRadiusCap(plan interfaces.IaCPlan, opts SecurityCheckOpts) []SecurityFinding {
	var findings []SecurityFinding
	maxChanges := opts.MaxChanges
	if maxChanges <= 0 {
		maxChanges = 20
	}

	// Check total action count.
	if len(plan.Actions) > maxChanges {
		findings = append(findings, SecurityFinding{
			RuleID: "R10", Severity: SeverityFail,
			Resource: "(plan)", Type: "(plan)",
			Message: fmt.Sprintf("plan contains %d actions which exceeds --max-changes=%d", len(plan.Actions), maxChanges),
		})
	}

	// Check for delete+create of the same stateful resource name.
	statefulTypes := map[string]bool{
		"infra.database": true,
		"infra.storage":  true,
	}
	deletes := map[string]bool{}
	creates := map[string]bool{}
	for i := range plan.Actions {
		if !statefulTypes[plan.Actions[i].Resource.Type] {
			continue
		}
		if plan.Actions[i].Action == "delete" {
			deletes[plan.Actions[i].Resource.Name] = true
		}
		if plan.Actions[i].Action == "create" {
			creates[plan.Actions[i].Resource.Name] = true
		}
	}
	for name := range deletes {
		if creates[name] {
			findings = append(findings, SecurityFinding{
				RuleID: "R10", Severity: SeverityFail,
				Resource: name, Type: "stateful",
				Message: fmt.Sprintf("plan deletes and recreates stateful resource %q — this destroys all data; use an in-place update instead, or perform a manual backup before any recreation", name),
			})
		}
	}
	return findings
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func toStringSlice(v any) []string {
	switch s := v.(type) {
	case []string:
		return s
	case []any:
		out := make([]string, 0, len(s))
		for _, item := range s {
			if str, ok := item.(string); ok {
				out = append(out, str)
			}
		}
		return out
	}
	return nil
}

func toAnySlice(v any) []any {
	switch s := v.(type) {
	case []any:
		return s
	case []map[string]any:
		out := make([]any, len(s))
		for i, m := range s {
			out[i] = m
		}
		return out
	}
	return nil
}

func boolFromAny(v any) bool {
	switch b := v.(type) {
	case bool:
		return b
	case string:
		return strings.EqualFold(b, "true")
	}
	return false
}

func intFromAny(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	}
	return 0
}
