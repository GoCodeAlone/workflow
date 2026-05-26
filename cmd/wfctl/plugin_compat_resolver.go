package main

import (
	"fmt"
	"os"
	"runtime"
	"slices"
	"strings"

	"golang.org/x/mod/semver"
)

const (
	PluginCompatModeEnforce = "enforce"
	PluginCompatModeWarn    = "warn"

	PluginCompatForceInstall = "force-install"
	PluginCompatForceUpdate  = "force-update"
	PluginCompatForceLock    = "force-lock"
	PluginCompatWarnReason   = "compat-mode=warn"
)

type PluginCompatResolverOptions struct {
	RequestedVersion string
	EngineVersion    string
	CompatMode       string
	Force            bool
	ForceReason      string
	Trust            CompatibilityTrustMode
	OS               string
	Arch             string
}

type PluginCompatDecision struct {
	Version  string
	Forced   bool
	Reason   string
	Warning  string
	Evidence *PluginCompatibilityEvidence
}

func ResolvePluginCompatibility(index *PluginVersionIndex, manifest *RegistryManifest, opts PluginCompatResolverOptions) (PluginCompatDecision, error) {
	if index == nil {
		return PluginCompatDecision{}, fmt.Errorf("compatibility index is required")
	}
	normalizedIndex, err := NormalizePluginVersionIndex(index, index.Plugin)
	if err != nil {
		return PluginCompatDecision{}, err
	}
	index = normalizedIndex
	engine, comparable := resolvePluginCompatEngineVersion(opts.EngineVersion)
	mode, err := parsePluginCompatMode(opts.CompatMode)
	if err != nil {
		return PluginCompatDecision{}, err
	}
	if opts.Trust == "" {
		opts.Trust = CompatibilityTrustAdvisory
	}
	if opts.OS == "" {
		opts.OS = runtime.GOOS
	}
	if opts.Arch == "" {
		opts.Arch = runtime.GOARCH
	}
	forceReason := opts.ForceReason
	if forceReason == "" {
		forceReason = PluginCompatForceInstall
	}

	versions := slices.Clone(index.Versions)
	sortCompatibilityIndex(&PluginVersionIndex{Versions: versions})
	if opts.RequestedVersion != "" {
		requested, err := CanonicalPluginVersion(opts.RequestedVersion)
		if err != nil {
			return PluginCompatDecision{}, err
		}
		for _, rec := range versions {
			if rec.Version == requested {
				return evaluatePluginCompatRecord(rec, index.EvidencePolicy, manifest, engine, comparable, mode, opts, forceReason)
			}
		}
		if manifest != nil && manifest.Version != "" && !shouldRequireCompatibilityEvidence(index.EvidencePolicy, engine, comparable, opts.Trust) {
			return PluginCompatDecision{Version: requested, Warning: "requested version not found in compatibility index; falling back to manifest pinning"}, nil
		}
		return PluginCompatDecision{}, fmt.Errorf("requested version %s not found in compatibility index for %s", requested, index.Plugin)
	}
	for _, rec := range versions {
		decision, err := evaluatePluginCompatRecord(rec, index.EvidencePolicy, manifest, engine, comparable, mode, opts, forceReason)
		if err == nil {
			return decision, nil
		}
		if !isKnownFailCompatError(err) {
			return PluginCompatDecision{}, err
		}
	}
	if manifest != nil && manifest.Version != "" {
		version, err := CanonicalPluginVersion(manifest.Version)
		if err == nil {
			return PluginCompatDecision{Version: version, Warning: "no compatible indexed version found; falling back to manifest version"}, nil
		}
	}
	return PluginCompatDecision{}, fmt.Errorf("no compatible version found for %s", index.Plugin)
}

func evaluatePluginCompatRecord(rec PluginVersionRecord, policy CompatibilityEvidencePolicy, manifest *RegistryManifest, engine string, comparable bool, mode string, opts PluginCompatResolverOptions, forceReason string) (PluginCompatDecision, error) {
	version, err := CanonicalPluginVersion(rec.Version)
	if err != nil {
		return PluginCompatDecision{}, err
	}
	if comparable && rec.MinEngineVersion != "" {
		minEngine, err := CanonicalEngineVersion(rec.MinEngineVersion)
		if err != nil {
			return PluginCompatDecision{}, fmt.Errorf("minEngineVersion for %s: %w", version, err)
		}
		if semver.Compare(engine, minEngine) < 0 {
			return PluginCompatDecision{}, fmt.Errorf("engine %s is below minEngineVersion %s for %s", engine, minEngine, version)
		}
	}
	archiveSHA := platformArchiveSHA(rec.Downloads, manifest, opts.OS, opts.Arch)
	requireEvidence := shouldRequireCompatibilityEvidence(policy, engine, comparable, opts.Trust)
	ev, ok := findCompatibilityEvidence(rec.Compatibility, engine, comparable, opts.OS, opts.Arch, archiveSHA, requireEvidence)
	if ok {
		if ev.Status == PluginCompatibilityStatusPass {
			return PluginCompatDecision{Version: version, Evidence: &ev}, nil
		}
		if opts.Force {
			return PluginCompatDecision{Version: version, Forced: true, Reason: forceReason, Evidence: &ev}, nil
		}
		if mode == PluginCompatModeWarn {
			return PluginCompatDecision{Version: version, Forced: true, Reason: PluginCompatWarnReason, Warning: "compatibility evidence is fail; continuing because compat-mode=warn", Evidence: &ev}, nil
		}
		return PluginCompatDecision{}, knownFailCompatError{version: version, engine: engine}
	}
	if requireEvidence {
		if opts.Force {
			return PluginCompatDecision{Version: version, Forced: true, Reason: forceReason, Warning: "missing required compatibility evidence; continuing because --force is set"}, nil
		}
		if mode == PluginCompatModeWarn {
			return PluginCompatDecision{Version: version, Forced: true, Reason: PluginCompatWarnReason, Warning: "missing required compatibility evidence; continuing because compat-mode=warn"}, nil
		}
		return PluginCompatDecision{}, fmt.Errorf("missing required compatibility evidence for %s on engine %s", version, engine)
	}
	decision := PluginCompatDecision{Version: version}
	if !comparable {
		decision.Warning = "local wfctl engine version is not comparable; compatibility evidence is advisory"
	} else if opts.Trust == CompatibilityTrustAdvisory {
		decision.Warning = "compatibility evidence is advisory for this registry"
	}
	return decision, nil
}

func parsePluginCompatMode(raw string) (string, error) {
	switch strings.TrimSpace(raw) {
	case "", PluginCompatModeEnforce:
		return PluginCompatModeEnforce, nil
	case PluginCompatModeWarn:
		return PluginCompatModeWarn, nil
	default:
		return "", fmt.Errorf("unsupported compat mode %q", raw)
	}
}

func resolvePluginCompatMode(cliValue string, cfg *RegistryConfig) (string, error) {
	if strings.TrimSpace(cliValue) != "" {
		return parsePluginCompatMode(cliValue)
	}
	if env := strings.TrimSpace(os.Getenv("WFCTL_PLUGIN_COMPAT_MODE")); env != "" {
		return parsePluginCompatMode(env)
	}
	if cfg != nil && strings.TrimSpace(cfg.Compatibility.Mode) != "" {
		return parsePluginCompatMode(cfg.Compatibility.Mode)
	}
	return PluginCompatModeEnforce, nil
}

func resolvePluginCompatEngineVersion(raw string) (string, bool) {
	if raw == "" {
		raw = strings.TrimSpace(os.Getenv("WFCTL_ENGINE_VERSION"))
	}
	if raw == "" {
		raw = buildVersion()
	}
	engine, err := CanonicalEngineVersion(raw)
	if err != nil {
		return "v0.0.0", false
	}
	return engine, true
}

func platformArchiveSHA(recordDownloads []PluginDownload, manifest *RegistryManifest, goos, goarch string) string {
	for _, d := range recordDownloads {
		if d.OS == goos && d.Arch == goarch {
			if sha, err := NormalizeSHA256Hex(d.SHA256); err == nil {
				return sha
			}
		}
	}
	if manifest != nil {
		for _, d := range manifest.Downloads {
			if d.OS == goos && d.Arch == goarch {
				if sha, err := NormalizeSHA256Hex(d.SHA256); err == nil {
					return sha
				}
			}
		}
	}
	return ""
}

func findCompatibilityEvidence(evidence []PluginCompatibilityEvidence, engine string, comparable bool, goos, goarch, archiveSHA string, requireArchive bool) (PluginCompatibilityEvidence, bool) {
	var rangeMatch *PluginCompatibilityEvidence
	for i := range evidence {
		ev := evidence[i]
		// Only typed-iac evidence satisfies registry readiness checks.
		// legacy-host-load evidence is advisory only and is intentionally
		// excluded here — it must never satisfy IaC compatibility decisions.
		if ev.Mode != PluginCompatibilityModeTypedIaC || ev.OS != goos || ev.Arch != goarch {
			continue
		}
		if requireArchive && (archiveSHA == "" || ev.ArchiveSHA256 == "") {
			continue
		}
		if archiveSHA == "" && ev.ArchiveSHA256 != "" {
			continue
		}
		if archiveSHA != "" && ev.ArchiveSHA256 != archiveSHA {
			continue
		}
		if comparable && ev.EngineVersion == engine {
			return ev, true
		}
		if comparable && ev.CompatibleEngineRange != nil &&
			semver.Compare(engine, ev.CompatibleEngineRange.Min) >= 0 &&
			semver.Compare(engine, ev.CompatibleEngineRange.Max) <= 0 {
			rangeMatch = &ev
		}
	}
	if rangeMatch != nil {
		return *rangeMatch, true
	}
	return PluginCompatibilityEvidence{}, false
}

func shouldRequireCompatibilityEvidence(policy CompatibilityEvidencePolicy, engine string, comparable bool, trust CompatibilityTrustMode) bool {
	if !comparable || trust != CompatibilityTrustFirstParty || policy.RequiredFromEngine == "" {
		return false
	}
	requiredFrom, err := CanonicalEngineVersion(policy.RequiredFromEngine)
	if err != nil {
		return false
	}
	return semver.Compare(engine, requiredFrom) >= 0
}

type knownFailCompatError struct {
	version string
	engine  string
}

func (e knownFailCompatError) Error() string {
	return fmt.Sprintf("compatibility evidence marks %s failed for engine %s", e.version, e.engine)
}

func isKnownFailCompatError(err error) bool {
	_, ok := err.(knownFailCompatError)
	return ok
}

func registryTrustMode(cfg *RegistryConfig, sourceName string) CompatibilityTrustMode {
	if cfg == nil {
		return CompatibilityTrustAdvisory
	}
	for i := range cfg.Registries {
		if cfg.Registries[i].Name == sourceName {
			if cfg.Registries[i].CompatibilityEvidence.Trust != "" {
				return cfg.Registries[i].CompatibilityEvidence.Trust
			}
			return CompatibilityTrustAdvisory
		}
	}
	return CompatibilityTrustAdvisory
}
