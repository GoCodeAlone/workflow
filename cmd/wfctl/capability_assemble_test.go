package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/capability/inventory"
)

func TestParseAssemblySet_FileAtFileInline(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "set.json")
	if err := os.WriteFile(p, []byte(`{"capabilities":["auth.authn"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, spec := range []string{p, "@" + p} {
		in, err := parseAssemblySet(spec, nil)
		if err != nil || len(in.Capabilities) != 1 || in.Capabilities[0] != "auth.authn" {
			t.Fatalf("spec=%q in=%+v err=%v", spec, in, err)
		}
	}
	// inline literal
	in, err := parseAssemblySet(`{"capabilities":["http.routing"]}`, nil)
	if err != nil || in.Capabilities[0] != "http.routing" {
		t.Fatalf("inline: %+v %v", in, err)
	}
}

func TestCanonicalIDs_RejectNonCanonicalWithDidYouMean(t *testing.T) {
	// "auth" (bare) is NOT a canonical id -> error mentioning candidate(s)
	err := validateCanonicalIDs([]string{"auth"}, taxonomyForTest(t))
	if err == nil || !strings.Contains(err.Error(), "auth.authn") {
		t.Fatalf("want did-you-mean mentioning auth.authn, got %v", err)
	}
}

func TestAssembleCLI_DeterministicEmission(t *testing.T) {
	// D14 path-safety rejects --out outside cwd unless --force is set; t.TempDir()
	// lives under the system temp root, so the test passes --force (the D14 escape
	// hatch) to scaffold into an absolute temp dir. The determinism assertion
	// (byte-identical workflow.yaml across two runs) is unchanged.
	out1 := t.TempDir()
	out2 := t.TempDir()
	set := filepath.Join(t.TempDir(), "set.json")
	os.WriteFile(set, []byte(`{"capabilities":["observability.health","http.routing"]}`), 0o600)
	var b bytes.Buffer
	for _, out := range []string{out1, out2} {
		if err := runCapabilityAssemble([]string{"--set", set, "--out", out, "--force"}, &b); err != nil {
			t.Fatal(err)
		}
	}
	w1, _ := os.ReadFile(filepath.Join(out1, "workflow.yaml"))
	w2, _ := os.ReadFile(filepath.Join(out2, "workflow.yaml"))
	if !bytes.Equal(w1, w2) {
		t.Fatalf("MC5 determinism: outputs differ\n%s\n%s", w1, w2)
	}
}

func TestAssembleCLI_OutPathRejectsSystemPathWithoutForce(t *testing.T) {
	var b bytes.Buffer
	set := filepath.Join(t.TempDir(), "set.json")
	os.WriteFile(set, []byte(`{"capabilities":["observability.health"]}`), 0o600)
	err := runCapabilityAssemble([]string{"--set", set, "--out", "/etc/cap-asm-test"}, &b)
	if err == nil {
		t.Fatal("want rejection of system/out-of-cwd --out without --force (D14)")
	}
}

// taxonomyForTest loads the real checked-in taxonomy (same default path the CLI uses).
func taxonomyForTest(t *testing.T) *inventory.Taxonomy {
	t.Helper()
	tax, err := inventory.LoadTaxonomy(defaultCapabilityTaxonomyPath())
	if err != nil {
		t.Fatalf("load taxonomy: %v", err)
	}
	return tax
}
