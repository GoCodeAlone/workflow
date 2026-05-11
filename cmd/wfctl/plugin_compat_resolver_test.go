package main

import (
	"strings"
	"testing"
)

func TestPluginCompatResolverNewestExactTrustedPassWins(t *testing.T) {
	idx := resolverIndex(
		resolverRecord("v0.1.0", passEvidence("v0.1.0", "v0.51.2")),
		resolverRecord("v0.2.0", passEvidence("v0.2.0", "v0.51.2")),
	)
	decision, err := ResolvePluginCompatibility(idx, nil, resolverOptions())
	if err != nil {
		t.Fatalf("ResolvePluginCompatibility: %v", err)
	}
	if decision.Version != "v0.2.0" || decision.Forced {
		t.Fatalf("decision = %#v, want latest pass v0.2.0", decision)
	}
}

func TestPluginCompatResolverCanonicalizesIndexVersionsBeforeSorting(t *testing.T) {
	idx := resolverIndex(
		resolverRecord("0.9.0", passEvidence("0.9.0", "v0.51.2")),
		resolverRecord("0.10.0", passEvidence("0.10.0", "v0.51.2")),
	)
	decision, err := ResolvePluginCompatibility(idx, nil, resolverOptions())
	if err != nil {
		t.Fatalf("ResolvePluginCompatibility: %v", err)
	}
	if decision.Version != "v0.10.0" {
		t.Fatalf("version = %s, want v0.10.0", decision.Version)
	}
}

func TestPluginCompatResolverNewerFailSkipsOlderPass(t *testing.T) {
	idx := resolverIndex(
		resolverRecord("v0.1.0", passEvidence("v0.1.0", "v0.51.2")),
		resolverRecord("v0.2.0", failEvidence("v0.2.0", "v0.51.2")),
	)
	decision, err := ResolvePluginCompatibility(idx, nil, resolverOptions())
	if err != nil {
		t.Fatalf("ResolvePluginCompatibility: %v", err)
	}
	if decision.Version != "v0.1.0" {
		t.Fatalf("version = %s, want v0.1.0", decision.Version)
	}
}

func TestPluginCompatResolverRequiredEvidenceMustBindArchive(t *testing.T) {
	ev := passEvidence("v0.2.0", "v0.51.2")
	ev.ArchiveSHA256 = strings.Repeat("a", 64)
	ev, err := ValidateCompatibilityEvidence(ev)
	if err != nil {
		t.Fatalf("ValidateCompatibilityEvidence: %v", err)
	}
	rec := resolverRecord("v0.2.0", ev)
	rec.Downloads = nil
	idx := resolverIndex(rec)
	idx.EvidencePolicy.RequiredFromEngine = "v0.51.0"
	_, err = ResolvePluginCompatibility(idx, &RegistryManifest{Version: "v0.2.0"}, resolverOptions())
	if err == nil {
		t.Fatal("expected missing required evidence when archive digest cannot be matched")
	}
	if !strings.Contains(err.Error(), "missing required compatibility evidence") {
		t.Fatalf("error = %v, want missing evidence context", err)
	}
}

func TestPluginCompatResolverRequestedKnownFailEnforces(t *testing.T) {
	idx := resolverIndex(resolverRecord("v0.2.0", failEvidence("v0.2.0", "v0.51.2")))
	_, err := ResolvePluginCompatibility(idx, nil, PluginCompatResolverOptions{
		RequestedVersion: "v0.2.0",
		EngineVersion:    "v0.51.2",
		CompatMode:       PluginCompatModeEnforce,
		Trust:            CompatibilityTrustFirstParty,
		OS:               "darwin",
		Arch:             "arm64",
	})
	if err == nil {
		t.Fatal("expected known-fail error")
	}
	if !strings.Contains(err.Error(), "failed") {
		t.Fatalf("error = %v, want failed context", err)
	}
}

func TestPluginCompatResolverWarnAndForcePermitKnownFail(t *testing.T) {
	idx := resolverIndex(resolverRecord("v0.2.0", failEvidence("v0.2.0", "v0.51.2")))
	warn, err := ResolvePluginCompatibility(idx, nil, PluginCompatResolverOptions{
		RequestedVersion: "v0.2.0",
		EngineVersion:    "v0.51.2",
		CompatMode:       PluginCompatModeWarn,
		Trust:            CompatibilityTrustFirstParty,
		OS:               "darwin",
		Arch:             "arm64",
	})
	if err != nil {
		t.Fatalf("warn ResolvePluginCompatibility: %v", err)
	}
	if !warn.Forced || warn.Reason != PluginCompatWarnReason {
		t.Fatalf("warn decision = %#v, want forced compat-mode=warn", warn)
	}
	forced, err := ResolvePluginCompatibility(idx, nil, PluginCompatResolverOptions{
		RequestedVersion: "v0.2.0",
		EngineVersion:    "v0.51.2",
		CompatMode:       PluginCompatModeEnforce,
		Force:            true,
		ForceReason:      PluginCompatForceUpdate,
		Trust:            CompatibilityTrustFirstParty,
		OS:               "darwin",
		Arch:             "arm64",
	})
	if err != nil {
		t.Fatalf("force ResolvePluginCompatibility: %v", err)
	}
	if !forced.Forced || forced.Reason != PluginCompatForceUpdate {
		t.Fatalf("force decision = %#v, want forced update reason", forced)
	}
}

func TestPluginCompatResolverMissingRequiredFirstPartyEvidenceBlocks(t *testing.T) {
	idx := resolverIndex(resolverRecord("v0.2.0"))
	idx.EvidencePolicy.RequiredFromEngine = "v0.51.0"
	_, err := ResolvePluginCompatibility(idx, nil, resolverOptions())
	if err == nil {
		t.Fatal("expected missing required evidence error")
	}
	if !strings.Contains(err.Error(), "missing required compatibility evidence") {
		t.Fatalf("error = %v, want missing evidence context", err)
	}
}

func TestPluginCompatResolverRequestedMissingFromRequiredIndexBlocks(t *testing.T) {
	idx := resolverIndex(resolverRecord("v0.2.0", passEvidence("v0.2.0", "v0.51.2")))
	idx.EvidencePolicy.RequiredFromEngine = "v0.51.0"
	_, err := ResolvePluginCompatibility(idx, &RegistryManifest{Version: "v0.2.0"}, PluginCompatResolverOptions{
		RequestedVersion: "v0.3.0",
		EngineVersion:    "v0.51.2",
		CompatMode:       PluginCompatModeEnforce,
		Trust:            CompatibilityTrustFirstParty,
		OS:               "darwin",
		Arch:             "arm64",
	})
	if err == nil {
		t.Fatal("expected requested missing required-index error")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("error = %v, want not found context", err)
	}
}

func TestPluginCompatResolverAdvisoryEvidenceFallsBackToMinEngine(t *testing.T) {
	idx := resolverIndex(resolverRecord("v0.2.0"))
	decision, err := ResolvePluginCompatibility(idx, nil, PluginCompatResolverOptions{
		EngineVersion: "v0.51.2",
		CompatMode:    PluginCompatModeEnforce,
		Trust:         CompatibilityTrustAdvisory,
		OS:            "darwin",
		Arch:          "arm64",
	})
	if err != nil {
		t.Fatalf("ResolvePluginCompatibility: %v", err)
	}
	if decision.Version != "v0.2.0" || decision.Warning == "" {
		t.Fatalf("decision = %#v, want advisory fallback warning", decision)
	}
}

func TestPluginCompatResolverPseudoLocalVersionIsAdvisory(t *testing.T) {
	idx := resolverIndex(resolverRecord("v0.2.0"))
	idx.EvidencePolicy.RequiredFromEngine = "v0.51.0"
	decision, err := ResolvePluginCompatibility(idx, nil, PluginCompatResolverOptions{
		EngineVersion: "dev",
		CompatMode:    PluginCompatModeEnforce,
		Trust:         CompatibilityTrustFirstParty,
		OS:            "darwin",
		Arch:          "arm64",
	})
	if err != nil {
		t.Fatalf("ResolvePluginCompatibility: %v", err)
	}
	if decision.Warning == "" {
		t.Fatalf("decision = %#v, want advisory warning", decision)
	}
}

func resolverOptions() PluginCompatResolverOptions {
	return PluginCompatResolverOptions{
		EngineVersion: "v0.51.2",
		CompatMode:    PluginCompatModeEnforce,
		Trust:         CompatibilityTrustFirstParty,
		OS:            "darwin",
		Arch:          "arm64",
	}
}

func resolverIndex(records ...PluginVersionRecord) *PluginVersionIndex {
	return &PluginVersionIndex{
		Plugin:   "workflow-plugin-test",
		Versions: records,
	}
}

func resolverRecord(version string, evidence ...PluginCompatibilityEvidence) PluginVersionRecord {
	return PluginVersionRecord{
		Version:          version,
		MinEngineVersion: "v0.50.0",
		Downloads: []PluginDownload{{
			OS:     "darwin",
			Arch:   "arm64",
			SHA256: testArchiveSHA256,
		}},
		Compatibility: evidence,
	}
}

func passEvidence(pluginVersion, engineVersion string) PluginCompatibilityEvidence {
	return resolverEvidence(pluginVersion, engineVersion, PluginCompatibilityStatusPass)
}

func failEvidence(pluginVersion, engineVersion string) PluginCompatibilityEvidence {
	return resolverEvidence(pluginVersion, engineVersion, PluginCompatibilityStatusFail)
}

func resolverEvidence(pluginVersion, engineVersion, status string) PluginCompatibilityEvidence {
	ev, err := ValidateCompatibilityEvidence(PluginCompatibilityEvidence{
		Plugin:        "workflow-plugin-test",
		Version:       pluginVersion,
		EngineVersion: engineVersion,
		Mode:          PluginCompatibilityModeTypedIaC,
		Status:        status,
		OS:            "darwin",
		Arch:          "arm64",
		ArchiveSHA256: testArchiveSHA256,
	})
	if err != nil {
		panic(err)
	}
	return ev
}
