package main

import (
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/dns/policy"
)

// TestRunDNSPolicy_usage pins the no-arg behavior: print usage + return
// an error so the wfctl dispatcher exits non-zero.
func TestRunDNSPolicy_usage(t *testing.T) {
	if err := runDNSPolicy([]string{}); err == nil {
		t.Fatal("expected error for no subcommand; got nil")
	}
}

func TestRunDNSPolicy_unknownSubcommand(t *testing.T) {
	err := runDNSPolicy([]string{"frobnicate"})
	if err == nil || !strings.Contains(err.Error(), "unknown subcommand") {
		t.Fatalf("expected unknown-subcommand error; got %v", err)
	}
}

// TestRunDNSPolicyShow_requiresZone + provider pins the basic flag-gates
// for the show subcommand; resolveDNSPolicyReader returns an error before
// any plugin lookup happens when --zone is missing. Catches the regression
// where the flag-required guard is silently skipped + the empty zone
// reaches the provider plugin as a remote lookup of "".
func TestRunDNSPolicyShow_requiresZone(t *testing.T) {
	err := runDNSPolicy([]string{"show", "--provider", "do-prod"})
	if err == nil || !strings.Contains(err.Error(), "--zone") {
		t.Fatalf("want --zone required error; got %v", err)
	}
}

func TestRunDNSPolicyShow_requiresProvider(t *testing.T) {
	err := runDNSPolicy([]string{"show", "--zone", "z.com"})
	if err == nil || !strings.Contains(err.Error(), "--provider") {
		t.Fatalf("want --provider required error; got %v", err)
	}
}

func TestRunDNSPolicySet_requiresOwner(t *testing.T) {
	err := runDNSPolicy([]string{"set", "--provider", "do-prod", "--zone", "z.com"})
	if err == nil || !strings.Contains(err.Error(), "--owner") {
		t.Fatalf("want --owner required error; got %v", err)
	}
}

func TestRunDNSPolicyTransfer_requiresName(t *testing.T) {
	err := runDNSPolicy([]string{"transfer-ownership", "--provider", "do-prod", "--zone", "z.com", "--new-owner", "ratchet"})
	if err == nil || !strings.Contains(err.Error(), "--name") {
		t.Fatalf("want --name required error; got %v", err)
	}
}

func TestRunDNSPolicyTransfer_requiresNewOwner(t *testing.T) {
	err := runDNSPolicy([]string{"transfer-ownership", "--provider", "do-prod", "--zone", "z.com", "--name", "www"})
	if err == nil || !strings.Contains(err.Error(), "--new-owner") {
		t.Fatalf("want --new-owner required error; got %v", err)
	}
}

func TestRunDNSPolicyDrift_requiresExpectFile(t *testing.T) {
	err := runDNSPolicy([]string{"drift", "--provider", "do-prod", "--zone", "z.com"})
	if err == nil || !strings.Contains(err.Error(), "--expect-file") {
		t.Fatalf("want --expect-file required error; got %v", err)
	}
}

// ── policy-mutation helper tests ──────────────────────────────────────────────

func TestMergeEntry_replacesSameOwner(t *testing.T) {
	existing := []policy.Entry{
		{Owner: "sre", Default: true},
		{Owner: "multisite", Patterns: []string{"www"}},
	}
	updated := policy.Entry{Owner: "multisite", Patterns: []string{"www", "admin"}}
	out := mergeEntry(existing, updated)
	if len(out) != 2 {
		t.Fatalf("want 2 entries; got %d: %+v", len(out), out)
	}
	if out[0].Owner != "sre" {
		t.Errorf("first entry owner = %q; want sre (order preserved)", out[0].Owner)
	}
	if out[1].Owner != "multisite" || len(out[1].Patterns) != 2 {
		t.Errorf("multisite entry not updated; got %+v", out[1])
	}
}

func TestMergeEntry_appendsNewOwner(t *testing.T) {
	existing := []policy.Entry{{Owner: "sre", Default: true}}
	updated := policy.Entry{Owner: "ratchet", Patterns: []string{"api"}}
	out := mergeEntry(existing, updated)
	if len(out) != 2 || out[1].Owner != "ratchet" {
		t.Errorf("append failed; got %+v", out)
	}
}

func TestTransferPatternOwnership_moves(t *testing.T) {
	entries := []policy.Entry{
		{Owner: "multisite", Patterns: []string{"www", "admin"}},
		{Owner: "ratchet", Patterns: []string{"api"}},
	}
	prev, out, err := transferPatternOwnership(entries, "www", "ratchet")
	if err != nil {
		t.Fatalf("transfer: %v", err)
	}
	if prev != "multisite" {
		t.Errorf("prevOwner = %q; want multisite", prev)
	}
	// multisite should now have only "admin"; ratchet should have "api" + "www"
	var multisitePatterns, ratchetPatterns []string
	for _, e := range out {
		switch e.Owner {
		case "multisite":
			multisitePatterns = e.Patterns
		case "ratchet":
			ratchetPatterns = e.Patterns
		}
	}
	if len(multisitePatterns) != 1 || multisitePatterns[0] != "admin" {
		t.Errorf("multisite patterns = %v; want [admin]", multisitePatterns)
	}
	if !containsStringDNSPolicy(ratchetPatterns, "www") || !containsStringDNSPolicy(ratchetPatterns, "api") {
		t.Errorf("ratchet patterns = %v; want both [api, www]", ratchetPatterns)
	}
}

func TestTransferPatternOwnership_dropsEmptyEntry(t *testing.T) {
	entries := []policy.Entry{
		{Owner: "old", Patterns: []string{"www"}}, // single-pattern entry
		{Owner: "new", Patterns: []string{"api"}},
	}
	prev, out, err := transferPatternOwnership(entries, "www", "new")
	if err != nil {
		t.Fatalf("transfer: %v", err)
	}
	if prev != "old" {
		t.Errorf("prevOwner = %q; want old", prev)
	}
	for _, e := range out {
		if e.Owner == "old" {
			t.Errorf("emptied 'old' entry should have been dropped; got %+v", e)
		}
	}
}

func TestTransferPatternOwnership_errorOnMissingPattern(t *testing.T) {
	entries := []policy.Entry{{Owner: "sre", Default: true}}
	_, _, err := transferPatternOwnership(entries, "ghost", "ratchet")
	if err == nil {
		t.Fatal("expected error for missing pattern; got nil")
	}
}

func TestComparePolicyEntries_detectsMissingExtraMismatch(t *testing.T) {
	expected := []policy.Entry{
		{Owner: "sre", Default: true},
		{Owner: "multisite", Patterns: []string{"www"}},
	}
	live := []policy.Entry{
		{Owner: "sre", Default: false},                // default flag mismatch
		{Owner: "ratchet", Patterns: []string{"api"}}, // extra
		// multisite missing
	}
	diffs := comparePolicyEntries(expected, live)
	if len(diffs) == 0 {
		t.Fatal("expected diffs; got none")
	}
	joined := strings.Join(diffs, "|")
	if !strings.Contains(joined, "MISSING") || !strings.Contains(joined, "multisite") {
		t.Errorf("missing-detection failed; diffs=%v", diffs)
	}
	if !strings.Contains(joined, "EXTRA") || !strings.Contains(joined, "ratchet") {
		t.Errorf("extra-detection failed; diffs=%v", diffs)
	}
	if !strings.Contains(joined, "MISMATCH default") {
		t.Errorf("default-mismatch detection failed; diffs=%v", diffs)
	}
}

func TestComparePolicyEntries_noDriftReturnsEmpty(t *testing.T) {
	entries := []policy.Entry{
		{Owner: "sre", Default: true},
		{Owner: "multisite", Patterns: []string{"www"}},
	}
	diffs := comparePolicyEntries(entries, entries)
	if len(diffs) != 0 {
		t.Errorf("identical policies should produce zero diffs; got %v", diffs)
	}
}

func TestPolicyDigest_orderIndependent(t *testing.T) {
	a := []string{"heritage=wfinfra-v1 o=sre d=true", "heritage=wfinfra-v1 o=multisite p=www"}
	b := []string{"heritage=wfinfra-v1 o=multisite p=www", "heritage=wfinfra-v1 o=sre d=true"}
	if policyDigest(a) != policyDigest(b) {
		t.Errorf("digest should be order-invariant; a=%q b=%q", policyDigest(a), policyDigest(b))
	}
}

func containsStringDNSPolicy(s []string, target string) bool {
	for _, v := range s {
		if v == target {
			return true
		}
	}
	return false
}
