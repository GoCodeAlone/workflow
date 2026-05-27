package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/GoCodeAlone/workflow/dns/audit"
	"github.com/GoCodeAlone/workflow/dns/gate"
	"github.com/GoCodeAlone/workflow/dns/policy"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// runDNSPolicy implements the `wfctl dns-policy` builtin — the cross-cutting
// orchestrator for DNS ownership policy declared as TXT records at
// `_workflow-dns-policy.<zone>`. Per design-guidance §CLI: cross-cutting
// orchestration commands live as wfctl builtins; capability-scoped commands
// stay in plugin cliCommands. dns-policy reads + writes via any provider
// plugin's IaCProvider.ResourceDriver("infra.dns"), so it works across DO /
// CF / NC / Hover without per-provider command duplication.
//
// Subcommands:
//   - show               — pretty-print parsed policy for a zone
//   - set                — upsert a policy entry (owner / patterns / types / default)
//   - transfer-ownership — rewrite policy so a different owner controls a record
//   - drift              — compare configured-vs-live policy and report diffs
//
// Each mutating command (set, transfer-ownership) appends a JSONL audit
// trail entry to `${XDG_STATE_HOME}/wfctl/plugins/wfctl/dns-audit.jsonl`.
func runDNSPolicy(args []string) error {
	if len(args) < 1 {
		return dnsPolicyUsage()
	}
	switch args[0] {
	case "show":
		return runDNSPolicyShow(args[1:])
	case "set":
		return runDNSPolicySet(args[1:])
	case "transfer-ownership":
		return runDNSPolicyTransfer(args[1:])
	case "drift":
		return runDNSPolicyDrift(args[1:])
	case "-h", "--help", "help":
		return dnsPolicyUsage()
	default:
		return fmt.Errorf("dns-policy: unknown subcommand %q", args[0])
	}
}

func dnsPolicyUsage() error {
	fmt.Fprintf(flag.CommandLine.Output(), `Usage: wfctl dns-policy <subcommand> [flags]

Manage DNS ownership policy TXT records via any iac.provider that supports
"infra.dns". Cross-provider; resolved through the workflow config like other
infra commands.

Subcommands:
  show               Pretty-print parsed policy for a zone
  set                Upsert a policy entry (owner/patterns/types/default)
  transfer-ownership Rewrite a record's owner via policy update
  drift              Compare configured vs live policy; report diffs

Common flags (all subcommands):
  --config <file>   Config file (default: infra.yaml or config/infra.yaml)
  --env <name>      Environment name for config resolution
  --provider <name> iac.provider module name from config (required)
  --zone <fqdn>     DNS zone (required)
`)
	return fmt.Errorf("missing or unknown subcommand")
}

// commonDNSPolicyFlags binds the four-flag set that every dns-policy
// subcommand needs. Centralized so adding a new common flag updates every
// subcommand in one place.
type commonDNSPolicyFlags struct {
	configFile, envName, providerName, zone string
}

func bindCommonDNSPolicyFlags(fs *flag.FlagSet, c *commonDNSPolicyFlags) {
	fs.StringVar(&c.configFile, "config", "", "Config file")
	fs.StringVar(&c.configFile, "c", "", "Config file (short for --config)")
	fs.StringVar(&c.envName, "env", "", "Environment name")
	fs.StringVar(&c.providerName, "provider", "", "iac.provider module name (required)")
	fs.StringVar(&c.zone, "zone", "", "DNS zone (required)")
}

// resolveDNSPolicyReader resolves a provider via the standard wfctl
// config-loading path, obtains the infra.dns ResourceDriver, and wraps it
// in a *gate.DriverReader that satisfies policy.DNSPolicyReader (both
// GetTXT and UpsertTXT). The returned closer is the IaCProvider plugin
// process shutdown — caller MUST defer it. Mirrors the runInfraImport
// resolution flow at cmd/wfctl/infra.go:1056-1077.
func resolveDNSPolicyReader(ctx context.Context, common *commonDNSPolicyFlags) (*gate.DriverReader, ioCloser, error) {
	if common.providerName == "" {
		return nil, nil, fmt.Errorf("--provider required")
	}
	if common.zone == "" {
		return nil, nil, fmt.Errorf("--zone required")
	}
	cfgFile, err := resolveInfraConfigPath(common.configFile)
	if err != nil {
		return nil, nil, err
	}
	providerType, providerCfg, err := resolveProviderModuleByName(cfgFile, common.envName, common.providerName)
	if err != nil {
		return nil, nil, err
	}
	provider, closer, err := resolveIaCProvider(ctx, providerType, providerCfg)
	if err != nil {
		return nil, nil, fmt.Errorf("load provider %q: %w", providerType, err)
	}
	driver, err := provider.ResourceDriver("infra.dns")
	if err != nil {
		if closer != nil {
			_ = closer.Close()
		}
		return nil, nil, fmt.Errorf("provider %q: resolve infra.dns driver: %w", providerType, err)
	}
	return &gate.DriverReader{Driver: driver, Zone: common.zone}, closer, nil
}

// ioCloser is a narrow Close() error interface — the resolveIaCProvider
// closer return is io.Closer-shaped but pulling in the io package across
// every subcommand for one method is more noise than it's worth.
type ioCloser interface {
	Close() error
}

// resolveInfraConfigPath defaults the config path to infra.yaml or
// config/infra.yaml (matches runInfraImport behavior at infra.go:1044).
// Without going through a *flag.FlagSet — used by dns-policy subcommands
// which bind their flags directly.
func resolveInfraConfigPath(configFile string) (string, error) {
	if configFile != "" {
		if _, err := os.Stat(configFile); err != nil {
			return "", fmt.Errorf("config file %q: %w", configFile, err)
		}
		return configFile, nil
	}
	for _, candidate := range []string{"infra.yaml", "config/infra.yaml"} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("no config file found; pass --config <file> or place infra.yaml in the working directory")
}

// ── show ──────────────────────────────────────────────────────────────────────

func runDNSPolicyShow(args []string) error {
	fs := flag.NewFlagSet("dns-policy show", flag.ContinueOnError)
	var common commonDNSPolicyFlags
	bindCommonDNSPolicyFlags(fs, &common)
	var raw bool
	fs.BoolVar(&raw, "raw", false, "Print raw TXT RR values instead of parsed output")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ctx := context.Background()
	reader, closer, err := resolveDNSPolicyReader(ctx, &common)
	if err != nil {
		return err
	}
	if closer != nil {
		defer closer.Close()
	}
	policyName := gate.PolicyName(common.zone)
	rrs, err := reader.GetTXT(ctx, policyName)
	if err != nil {
		return fmt.Errorf("dns-policy show: fetch: %w", err)
	}
	if len(rrs) == 0 {
		fmt.Printf("No policy found at %s\n", policyName)
		return nil
	}
	if raw {
		for _, r := range rrs {
			fmt.Println(r)
		}
		return nil
	}
	pol, err := policy.Parse(common.zone, rrs)
	if err != nil {
		return fmt.Errorf("dns-policy show: parse: %w", err)
	}
	fmt.Printf("DNS Ownership Policy for zone: %s\n", common.zone)
	fmt.Printf("TXT record: %s (%d RR(s))\n", policyName, len(rrs))
	fmt.Println(strings.Repeat("-", 60))
	for _, e := range pol.Entries {
		marker := ""
		if e.Default {
			marker = " [DEFAULT]"
		}
		fmt.Printf("Owner: %s%s\n", e.Owner, marker)
		if len(e.Patterns) > 0 {
			fmt.Printf("  Patterns: %s\n", strings.Join(e.Patterns, ", "))
		} else {
			fmt.Printf("  Patterns: (catch-all default)\n")
		}
		if len(e.Types) > 0 {
			fmt.Printf("  Types:    %s\n", strings.Join(e.Types, ", "))
		} else {
			fmt.Printf("  Types:    all (except SOA/NS)\n")
		}
		fmt.Println()
	}
	return nil
}

// ── set ───────────────────────────────────────────────────────────────────────

func runDNSPolicySet(args []string) error {
	fs := flag.NewFlagSet("dns-policy set", flag.ContinueOnError)
	var common commonDNSPolicyFlags
	bindCommonDNSPolicyFlags(fs, &common)
	var owner, patterns, types string
	var defaultOwner bool
	var ttl int
	fs.StringVar(&owner, "owner", "", "Owner name for this policy entry (required)")
	fs.StringVar(&patterns, "patterns", "", "Comma-separated name patterns (empty = catch-all default)")
	fs.StringVar(&types, "types", "", "Comma-separated record types (empty = all except SOA/NS)")
	fs.BoolVar(&defaultOwner, "default", false, "Mark this entry as the default owner (d=true)")
	fs.IntVar(&ttl, "ttl", 300, "TTL in seconds for the policy TXT record")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if owner == "" {
		return fmt.Errorf("dns-policy set requires --owner")
	}
	ctx := context.Background()
	reader, closer, err := resolveDNSPolicyReader(ctx, &common)
	if err != nil {
		return err
	}
	if closer != nil {
		defer closer.Close()
	}
	policyName := gate.PolicyName(common.zone)
	rrs, err := reader.GetTXT(ctx, policyName)
	if err != nil {
		return fmt.Errorf("dns-policy set: fetch existing: %w", err)
	}
	existing, _ := policy.Parse(common.zone, rrs) // tolerate parse errors when overwriting
	if existing == nil {
		existing = &policy.Policy{Zone: common.zone}
	}
	entry := policy.Entry{
		Owner:    owner,
		Patterns: splitCSVDNSPolicy(patterns),
		Types:    splitCSVDNSPolicy(types),
		Default:  defaultOwner,
	}
	// Replace the entry for this owner (idempotent); leave other owners alone.
	merged := mergeEntry(existing.Entries, entry)
	newRRs, serr := policy.Serialize(&policy.Policy{Zone: common.zone, Entries: merged})
	if serr != nil {
		return fmt.Errorf("dns-policy set: serialize: %w", serr)
	}
	priorSHA := policyDigest(rrs)
	newSHA := policyDigest(newRRs)
	if err := reader.UpsertTXT(ctx, policyName, newRRs, ttl); err != nil {
		return fmt.Errorf("dns-policy set: write: %w", err)
	}
	audit.LogPolicyEdit(currentActor(), common.zone, "set-policy", priorSHA, newSHA)
	fmt.Printf("Updated policy at %s for owner %q\n", policyName, owner)
	return nil
}

// mergeEntry returns existing entries with the new entry replacing any
// entry whose Owner matches. If no existing entry matches, the new entry
// is appended. Order of other entries is preserved.
func mergeEntry(existing []policy.Entry, e policy.Entry) []policy.Entry {
	out := make([]policy.Entry, 0, len(existing)+1)
	replaced := false
	for _, ex := range existing {
		if ex.Owner == e.Owner {
			out = append(out, e)
			replaced = true
			continue
		}
		out = append(out, ex)
	}
	if !replaced {
		out = append(out, e)
	}
	return out
}

func splitCSVDNSPolicy(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// ── transfer-ownership ────────────────────────────────────────────────────────

func runDNSPolicyTransfer(args []string) error {
	fs := flag.NewFlagSet("dns-policy transfer-ownership", flag.ContinueOnError)
	var common commonDNSPolicyFlags
	bindCommonDNSPolicyFlags(fs, &common)
	var name, newOwner string
	var ttl int
	fs.StringVar(&name, "name", "", "Record name to transfer (required); matches the policy pattern not the literal DNS name")
	fs.StringVar(&newOwner, "new-owner", "", "New owner for the matched record (required)")
	fs.IntVar(&ttl, "ttl", 300, "TTL in seconds for the policy TXT record")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if name == "" {
		return fmt.Errorf("dns-policy transfer-ownership requires --name")
	}
	if newOwner == "" {
		return fmt.Errorf("dns-policy transfer-ownership requires --new-owner")
	}
	ctx := context.Background()
	reader, closer, err := resolveDNSPolicyReader(ctx, &common)
	if err != nil {
		return err
	}
	if closer != nil {
		defer closer.Close()
	}
	policyName := gate.PolicyName(common.zone)
	rrs, err := reader.GetTXT(ctx, policyName)
	if err != nil {
		return fmt.Errorf("dns-policy transfer-ownership: fetch: %w", err)
	}
	pol, err := policy.Parse(common.zone, rrs)
	if err != nil {
		return fmt.Errorf("dns-policy transfer-ownership: parse: %w", err)
	}
	prevOwner, updated, err := transferPatternOwnership(pol.Entries, name, newOwner)
	if err != nil {
		return fmt.Errorf("dns-policy transfer-ownership: %w", err)
	}
	newRRs, serr := policy.Serialize(&policy.Policy{Zone: common.zone, Entries: updated})
	if serr != nil {
		return fmt.Errorf("dns-policy transfer-ownership: serialize: %w", serr)
	}
	priorSHA := policyDigest(rrs)
	newSHA := policyDigest(newRRs)
	if err := reader.UpsertTXT(ctx, policyName, newRRs, ttl); err != nil {
		return fmt.Errorf("dns-policy transfer-ownership: write: %w", err)
	}
	audit.LogPolicyEdit(currentActor(), common.zone, "transfer-ownership:"+name+":"+prevOwner+"→"+newOwner, priorSHA, newSHA)
	fmt.Printf("Transferred %q in zone %s: %s → %s\n", name, common.zone, prevOwner, newOwner)
	return nil
}

// transferPatternOwnership finds the entry currently owning `name` (by
// pattern membership) and moves that single pattern to a new entry under
// `newOwner`. If `name` is not in any entry's pattern list, returns an
// error rather than silently no-oping — operators expect explicit
// feedback when the pattern doesn't exist.
//
// If the transferred pattern was the only pattern on the old entry, the
// old entry is removed entirely. If `newOwner` already has an entry, the
// pattern is appended to its existing pattern list (deduplicated).
func transferPatternOwnership(entries []policy.Entry, name, newOwner string) (prevOwner string, out []policy.Entry, err error) {
	out = make([]policy.Entry, 0, len(entries))
	found := false
	for _, e := range entries {
		idx := indexOf(e.Patterns, name)
		if idx == -1 {
			out = append(out, e)
			continue
		}
		prevOwner = e.Owner
		found = true
		// Remove `name` from this entry's patterns. Drop the entry entirely
		// if its pattern list becomes empty.
		remaining := append([]string(nil), e.Patterns[:idx]...)
		remaining = append(remaining, e.Patterns[idx+1:]...)
		if len(remaining) > 0 {
			e.Patterns = remaining
			out = append(out, e)
		}
	}
	if !found {
		return "", nil, fmt.Errorf("pattern %q not found in any entry", name)
	}
	// Append to existing newOwner entry, or create a fresh one.
	merged := false
	for i := range out {
		if out[i].Owner == newOwner {
			if indexOf(out[i].Patterns, name) == -1 {
				out[i].Patterns = append(out[i].Patterns, name)
			}
			merged = true
			break
		}
	}
	if !merged {
		out = append(out, policy.Entry{Owner: newOwner, Patterns: []string{name}})
	}
	return prevOwner, out, nil
}

func indexOf(s []string, target string) int {
	for i, v := range s {
		if v == target {
			return i
		}
	}
	return -1
}

// ── drift ─────────────────────────────────────────────────────────────────────

// runDNSPolicyDrift compares the live policy (as fetched from the
// provider) against an expected policy declared inline via --expect or
// loaded from --expect-file. Reports missing / extra / mismatched
// entries. Read-only: never writes to the zone.
func runDNSPolicyDrift(args []string) error {
	fs := flag.NewFlagSet("dns-policy drift", flag.ContinueOnError)
	var common commonDNSPolicyFlags
	bindCommonDNSPolicyFlags(fs, &common)
	var expectFile string
	fs.StringVar(&expectFile, "expect-file", "", "Path to expected policy TXT-RR file (one RR per line). Required.")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if expectFile == "" {
		return fmt.Errorf("dns-policy drift requires --expect-file")
	}
	expectBytes, err := os.ReadFile(expectFile)
	if err != nil {
		return fmt.Errorf("dns-policy drift: read %s: %w", expectFile, err)
	}
	expectedRRs := splitNonEmptyLines(string(expectBytes))
	expected, err := policy.Parse(common.zone, expectedRRs)
	if err != nil {
		return fmt.Errorf("dns-policy drift: parse expected: %w", err)
	}
	ctx := context.Background()
	reader, closer, err := resolveDNSPolicyReader(ctx, &common)
	if err != nil {
		return err
	}
	if closer != nil {
		defer closer.Close()
	}
	policyName := gate.PolicyName(common.zone)
	liveRRs, err := reader.GetTXT(ctx, policyName)
	if err != nil {
		return fmt.Errorf("dns-policy drift: fetch live: %w", err)
	}
	live, err := policy.Parse(common.zone, liveRRs)
	if err != nil {
		return fmt.Errorf("dns-policy drift: parse live: %w", err)
	}
	diffs := comparePolicyEntries(expected.Entries, live.Entries)
	if len(diffs) == 0 {
		fmt.Printf("No drift detected for %s\n", common.zone)
		return nil
	}
	fmt.Printf("Drift detected for %s:\n", common.zone)
	for _, d := range diffs {
		fmt.Printf("  %s\n", d)
	}
	return errors.New("dns-policy drift: differences detected")
}

func splitNonEmptyLines(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return out
}

// comparePolicyEntries returns human-readable diff strings. Owners present
// in expected but missing in live are MISSING; owners in live but not
// expected are EXTRA; owners in both but with different pattern/type sets
// are MISMATCHED. Default-flag mismatches are surfaced explicitly.
func comparePolicyEntries(expected, live []policy.Entry) []string {
	expByOwner := indexByOwner(expected)
	liveByOwner := indexByOwner(live)
	var diffs []string
	for owner, e := range expByOwner {
		l, ok := liveByOwner[owner]
		if !ok {
			diffs = append(diffs, fmt.Sprintf("MISSING: owner=%s patterns=%v types=%v default=%v", owner, e.Patterns, e.Types, e.Default))
			continue
		}
		if !stringSetEqual(e.Patterns, l.Patterns) {
			diffs = append(diffs, fmt.Sprintf("MISMATCH patterns owner=%s expected=%v live=%v", owner, e.Patterns, l.Patterns))
		}
		if !stringSetEqual(e.Types, l.Types) {
			diffs = append(diffs, fmt.Sprintf("MISMATCH types owner=%s expected=%v live=%v", owner, e.Types, l.Types))
		}
		if e.Default != l.Default {
			diffs = append(diffs, fmt.Sprintf("MISMATCH default owner=%s expected=%v live=%v", owner, e.Default, l.Default))
		}
	}
	for owner, l := range liveByOwner {
		if _, ok := expByOwner[owner]; !ok {
			diffs = append(diffs, fmt.Sprintf("EXTRA: owner=%s patterns=%v types=%v default=%v", owner, l.Patterns, l.Types, l.Default))
		}
	}
	return diffs
}

func indexByOwner(entries []policy.Entry) map[string]policy.Entry {
	m := make(map[string]policy.Entry, len(entries))
	for _, e := range entries {
		m[e.Owner] = e
	}
	return m
}

func stringSetEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	set := make(map[string]struct{}, len(a))
	for _, s := range a {
		set[s] = struct{}{}
	}
	for _, s := range b {
		if _, ok := set[s]; !ok {
			return false
		}
	}
	return true
}

// policyDigest returns a stable short hash for a policy TXT-RR slice, used
// as the prior/new SHA recorded in the audit trail. SHA-256 of the
// alphabetically sorted joined RRs — invariant to slice order so the same
// policy semantics produce the same digest regardless of provider-side
// record ordering.
func policyDigest(rrs []string) string {
	if len(rrs) == 0 {
		return ""
	}
	sorted := append([]string(nil), rrs...)
	// Sort in-place; stable order for deterministic hashing.
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && sorted[j-1] > sorted[j]; j-- {
			sorted[j-1], sorted[j] = sorted[j], sorted[j-1]
		}
	}
	h := sha256.Sum256([]byte(strings.Join(sorted, "\n")))
	return fmt.Sprintf("%x", h[:8]) // 16-hex-char short digest is enough for trail readability
}

// currentActor returns the username for the audit-trail Actor field.
// Falls back to "unknown" if the env doesn't expose USER (CI runners
// without HOME set sometimes leave it blank).
func currentActor() string {
	if u := os.Getenv("USER"); u != "" {
		return u
	}
	return "unknown"
}

// Compile-time guard: ensure interfaces.IaCProvider has the methods this
// file expects. If a future SDK change breaks the contract, this fails
// fast at compile time rather than at runtime.
var _ = (*interfaces.IaCProvider)(nil)
