package main

import (
	"encoding/json"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestPluginCompatVersionCanonicalization(t *testing.T) {
	for _, input := range []string{"0.51.2", "v0.51.2"} {
		got, err := CanonicalEngineVersion(input)
		if err != nil {
			t.Fatalf("CanonicalEngineVersion(%q): %v", input, err)
		}
		if got != "v0.51.2" {
			t.Fatalf("CanonicalEngineVersion(%q) = %q, want v0.51.2", input, got)
		}
	}

	got, err := CanonicalPluginVersion("1.2.3")
	if err != nil {
		t.Fatalf("CanonicalPluginVersion: %v", err)
	}
	if got != "v1.2.3" {
		t.Fatalf("CanonicalPluginVersion = %q, want v1.2.3", got)
	}
}

func TestPluginCompatVersionRejectsInvalid(t *testing.T) {
	for _, input := range []string{"main", "v0.0.0-20260510", "1.2", "v1.2.3+meta"} {
		if _, err := CanonicalEngineVersion(input); err == nil {
			t.Fatalf("CanonicalEngineVersion(%q) succeeded, want error", input)
		}
	}
}

func TestPluginCompatDigestOmitsEvidenceDigest(t *testing.T) {
	ev := PluginCompatibilityEvidence{
		Plugin:               "workflow-plugin-test",
		Version:              "v0.1.0",
		EngineVersion:        "v0.51.2",
		WfctlVersion:         "v0.51.2",
		Mode:                 PluginCompatibilityModeTypedIaC,
		Status:               PluginCompatibilityStatusPass,
		EvidenceDigest:       "sha256:old",
		OS:                   "linux",
		Arch:                 "amd64",
		ArchiveSHA256:        strings.Repeat("a", 64),
		BinarySHA256:         strings.Repeat("b", 64),
		PluginManifestSHA256: strings.Repeat("c", 64),
		Repository:           "GoCodeAlone/workflow-plugin-test",
		GeneratedBy:          "wfctl plugin conformance",
	}

	got, err := ComputeEvidenceDigest(ev)
	if err != nil {
		t.Fatalf("ComputeEvidenceDigest: %v", err)
	}
	if got == "" || !strings.HasPrefix(got, "sha256:") {
		t.Fatalf("digest = %q, want sha256 prefix", got)
	}

	ev.EvidenceDigest = "sha256:different"
	got2, err := ComputeEvidenceDigest(ev)
	if err != nil {
		t.Fatalf("ComputeEvidenceDigest second: %v", err)
	}
	if got != got2 {
		t.Fatalf("digest changed when only evidenceDigest changed: %q vs %q", got, got2)
	}

	data, err := canonicalJSONWithoutEvidenceDigest(ev)
	if err != nil {
		t.Fatalf("canonicalJSONWithoutEvidenceDigest: %v", err)
	}
	if strings.Contains(string(data), "evidenceDigest") {
		t.Fatalf("canonical JSON contains evidenceDigest: %s", string(data))
	}
}

func TestPluginCompatSHA256Normalization(t *testing.T) {
	upper := strings.Repeat("A", 64)
	got, err := NormalizeSHA256Hex(upper)
	if err != nil {
		t.Fatalf("NormalizeSHA256Hex: %v", err)
	}
	if got != strings.ToLower(upper) {
		t.Fatalf("NormalizeSHA256Hex = %q, want lowercase", got)
	}
	for _, input := range []string{"", "abc", strings.Repeat("g", 64)} {
		if _, err := NormalizeSHA256Hex(input); err == nil {
			t.Fatalf("NormalizeSHA256Hex(%q) succeeded, want error", input)
		}
	}
}

func TestPluginCompatTrustParsing(t *testing.T) {
	var cfg RegistryConfig
	data := []byte(`
registries:
  - name: default
    type: github
    owner: GoCodeAlone
    repo: workflow-registry
    compatibilityEvidence:
      trust: first_party
  - name: community
    type: static
    url: https://example.test
    compatibilityEvidence:
      trust: advisory
`)
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("unmarshal trust config: %v", err)
	}
	if cfg.Registries[0].CompatibilityEvidence.Trust != CompatibilityTrustFirstParty {
		t.Fatalf("first trust = %q", cfg.Registries[0].CompatibilityEvidence.Trust)
	}
	if cfg.Registries[1].CompatibilityEvidence.Trust != CompatibilityTrustAdvisory {
		t.Fatalf("second trust = %q", cfg.Registries[1].CompatibilityEvidence.Trust)
	}
}

func TestPluginCompatTrustRejectsSigned(t *testing.T) {
	var cfg RegistryConfig
	data := []byte(`
registries:
  - name: signed
    type: static
    url: https://example.test
    compatibilityEvidence:
      trust: signed
`)
	if err := yaml.Unmarshal(data, &cfg); err == nil {
		t.Fatal("unmarshal signed trust succeeded, want error")
	}
}

func TestPluginCompatEvidenceValidation(t *testing.T) {
	ev := PluginCompatibilityEvidence{
		Plugin:        "workflow-plugin-test",
		Version:       "0.1.0",
		EngineVersion: "0.51.2",
		WfctlVersion:  "0.51.2",
		Mode:          PluginCompatibilityModeTypedIaC,
		Status:        PluginCompatibilityStatusPass,
		OS:            "linux",
		Arch:          "amd64",
		ArchiveSHA256: strings.Repeat("A", 64),
		BinarySHA256:  strings.Repeat("B", 64),
	}
	got, err := ValidateCompatibilityEvidence(ev)
	if err != nil {
		t.Fatalf("ValidateCompatibilityEvidence: %v", err)
	}
	if got.Version != "v0.1.0" || got.EngineVersion != "v0.51.2" {
		t.Fatalf("versions not canonicalized: %#v", got)
	}
	if got.ArchiveSHA256 != strings.Repeat("a", 64) {
		t.Fatalf("archive hash = %q", got.ArchiveSHA256)
	}
	if got.EvidenceDigest == "" {
		t.Fatal("EvidenceDigest not populated")
	}
	if _, err := json.Marshal(got); err != nil {
		t.Fatalf("marshal normalized evidence: %v", err)
	}
}
