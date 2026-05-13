package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/mod/semver"
)

type evidenceFlag []string

func (f *evidenceFlag) String() string {
	return strings.Join(*f, ",")
}

func (f *evidenceFlag) Set(value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("--evidence path must not be empty")
	}
	*f = append(*f, value)
	return nil
}

func runRegistryCompatibility(args []string) error {
	if len(args) < 1 {
		return registryCompatibilityUsage()
	}
	switch args[0] {
	case "update":
		return runRegistryCompatibilityUpdate(args[1:])
	default:
		return registryCompatibilityUsage()
	}
}

func registryCompatibilityUsage() error {
	fmt.Fprintf(flag.CommandLine.Output(), `Usage: wfctl plugin-registry compatibility <subcommand> [options]

Subcommands:
  update  Update compatibility/<plugin>/index.json from evidence files
`)
	return fmt.Errorf("registry compatibility subcommand is required")
}

func runRegistryCompatibilityUpdate(args []string) error {
	fs := flag.NewFlagSet("plugin-registry compatibility update", flag.ContinueOnError)
	registryDir := fs.String("registry-dir", "", "Path to local plugin registry checkout")
	pluginName := fs.String("plugin", "", "Plugin name")
	version := fs.String("version", "", "Plugin version")
	deriveRanges := fs.Bool("derive-ranges", false, "Derive pass ranges from enumerated evidence")
	latestEngine := fs.String("latest-engine", "", "Latest engine version used to mark stale evidence")
	var evidence evidenceFlag
	fs.Var(&evidence, "evidence", "Path to compatibility evidence JSON (repeatable)")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: wfctl plugin-registry compatibility update --registry-dir <dir> --plugin <name> --version <version> --evidence <file> [--evidence <file>]\n\nUpdate a plugin catalog compatibility index atomically.\n\nFlags: --registry-dir --plugin --version --evidence --derive-ranges --latest-engine\n\nOptions:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *registryDir == "" || *pluginName == "" || *version == "" {
		fs.Usage()
		return fmt.Errorf("--registry-dir, --plugin, and --version are required")
	}
	if len(evidence) == 0 {
		return fmt.Errorf("at least one --evidence file is required")
	}
	return updateRegistryCompatibilityIndex(registryCompatibilityUpdateOptions{
		RegistryDir:  *registryDir,
		Plugin:       *pluginName,
		Version:      *version,
		Evidence:     evidence,
		DeriveRanges: *deriveRanges,
		LatestEngine: *latestEngine,
	})
}

type registryCompatibilityUpdateOptions struct {
	RegistryDir  string
	Plugin       string
	Version      string
	Evidence     []string
	DeriveRanges bool
	LatestEngine string
}

func updateRegistryCompatibilityIndex(opts registryCompatibilityUpdateOptions) error {
	pluginName := strings.TrimSpace(opts.Plugin)
	version, err := CanonicalPluginVersion(opts.Version)
	if err != nil {
		return err
	}
	manifest, err := loadRegistryCompatibilityManifest(opts.RegistryDir, pluginName)
	if err != nil {
		return err
	}
	if manifest.Name != pluginName && normalizePluginName(manifest.Name) != normalizePluginName(pluginName) {
		return fmt.Errorf("manifest plugin %q does not match --plugin %q", manifest.Name, opts.Plugin)
	}
	manifestVersion, err := CanonicalPluginVersion(manifest.Version)
	if err != nil {
		return fmt.Errorf("manifest version: %w", err)
	}
	if manifestVersion != version {
		return fmt.Errorf("manifest version %s does not match --version %s", manifestVersion, version)
	}

	validated := make([]PluginCompatibilityEvidence, 0, len(opts.Evidence))
	for _, path := range opts.Evidence {
		ev, err := loadRegistryCompatibilityEvidence(path)
		if err != nil {
			return err
		}
		if ev.Plugin != pluginName && normalizePluginName(ev.Plugin) != normalizePluginName(pluginName) {
			return fmt.Errorf("evidence plugin %q does not match --plugin %q", ev.Plugin, opts.Plugin)
		}
		if ev.Version != version {
			return fmt.Errorf("evidence version %s does not match --version %s", ev.Version, version)
		}
		// IaC provider manifests require typed-iac conformance evidence only.
		// legacy-host-load evidence is advisory/legacy and cannot satisfy
		// typed-IaC registry readiness; reject it at index-update time so that
		// the registry index never contains evidence that looks valid but would
		// be silently ignored by the resolver.
		if manifestAdvertisesIaCProvider(manifest) && ev.Mode != PluginCompatibilityModeTypedIaC {
			return fmt.Errorf(
				"plugin %q advertises iacProvider capability: only typed-iac conformance evidence satisfies IaC registry readiness; "+
					"evidence %q has mode=%q (advisory/legacy only). "+
					"Run: wfctl plugin conformance --mode typed-iac --artifact <archive> to generate valid evidence",
				pluginName, path, ev.Mode,
			)
		}
		if err := validateEvidenceArchiveMatchesDownload(ev, manifest); err != nil {
			return err
		}
		validated = append(validated, ev)
	}

	indexPath := filepath.Join(opts.RegistryDir, "compatibility", pluginName, "index.json")
	index, err := loadRegistryCompatibilityIndex(indexPath, pluginName)
	if err != nil {
		return err
	}
	record := buildCompatibilityVersionRecord(version, manifest, validated)
	upsertCompatibilityRecord(index, record)
	sortCompatibilityIndex(index)
	if opts.DeriveRanges {
		deriveCompatibilityRanges(index)
		sortCompatibilityIndex(index)
	}
	if opts.LatestEngine != "" {
		latest, err := CanonicalEngineVersion(opts.LatestEngine)
		if err != nil {
			return fmt.Errorf("latest engine: %w", err)
		}
		index.EvidencePolicy.LatestEngine = latest
		index.EvidencePolicy.Stale = compatibilityIndexIsStale(index, latest)
	}

	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal compatibility index: %w", err)
	}
	if err := atomicWriteFile(indexPath, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write compatibility index: %w", err)
	}
	fmt.Printf("Updated compatibility index %s\n", indexPath)
	return nil
}

func loadRegistryCompatibilityManifest(registryDir, plugin string) (*RegistryManifest, error) {
	path := filepath.Join(registryDir, "plugins", plugin, "manifest.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read plugin manifest: %w", err)
	}
	var manifest RegistryManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("parse plugin manifest: %w", err)
	}
	return &manifest, nil
}

func loadRegistryCompatibilityEvidence(path string) (PluginCompatibilityEvidence, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return PluginCompatibilityEvidence{}, fmt.Errorf("read evidence %s: %w", path, err)
	}
	var ev PluginCompatibilityEvidence
	if err := json.Unmarshal(data, &ev); err != nil {
		return PluginCompatibilityEvidence{}, fmt.Errorf("parse evidence %s: %w", path, err)
	}
	ev, err = ValidateCompatibilityEvidence(ev)
	if err != nil {
		return PluginCompatibilityEvidence{}, fmt.Errorf("validate evidence %s: %w", path, err)
	}
	return ev, nil
}

func loadRegistryCompatibilityIndex(path, plugin string) (*PluginVersionIndex, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &PluginVersionIndex{Plugin: plugin}, nil
		}
		return nil, fmt.Errorf("read compatibility index: %w", err)
	}
	var index PluginVersionIndex
	if err := json.Unmarshal(data, &index); err != nil {
		return nil, fmt.Errorf("parse compatibility index: %w", err)
	}
	if index.Plugin == "" {
		index.Plugin = plugin
	}
	if index.Plugin != plugin && normalizePluginName(index.Plugin) != normalizePluginName(plugin) {
		return nil, fmt.Errorf("compatibility index plugin %q does not match %q", index.Plugin, plugin)
	}
	index.Plugin = plugin
	normalized, err := NormalizePluginVersionIndex(&index, plugin)
	if err != nil {
		return nil, fmt.Errorf("normalize compatibility index: %w", err)
	}
	return normalized, nil
}

func buildCompatibilityVersionRecord(version string, manifest *RegistryManifest, evidence []PluginCompatibilityEvidence) PluginVersionRecord {
	rec := PluginVersionRecord{
		Version:          version,
		MinEngineVersion: manifest.MinEngineVersion,
		Downloads:        normalizeCompatibilityDownloads(manifest.Downloads),
		Compatibility:    dedupeCompatibilityEvidence(evidence),
	}
	if rec.MinEngineVersion != "" {
		if canonical, err := CanonicalEngineVersion(rec.MinEngineVersion); err == nil {
			rec.MinEngineVersion = canonical
		}
	}
	sortCompatibilityEvidence(rec.Compatibility)
	return rec
}

func normalizeCompatibilityDownloads(downloads []PluginDownload) []PluginDownload {
	out := slices.Clone(downloads)
	for i := range out {
		if out[i].SHA256 == "" {
			continue
		}
		if normalized, err := NormalizeSHA256Hex(out[i].SHA256); err == nil {
			out[i].SHA256 = normalized
		}
	}
	return out
}

func validateEvidenceArchiveMatchesDownload(ev PluginCompatibilityEvidence, manifest *RegistryManifest) error {
	if ev.ArchiveSHA256 == "" {
		return fmt.Errorf("evidence archiveSHA256 is required for registry compatibility updates")
	}
	matchedPlatform := false
	for _, download := range manifest.Downloads {
		if download.OS != ev.OS || download.Arch != ev.Arch || download.SHA256 == "" {
			continue
		}
		matchedPlatform = true
		sha, err := NormalizeSHA256Hex(download.SHA256)
		if err != nil {
			return fmt.Errorf("manifest download sha256 for %s/%s: %w", download.OS, download.Arch, err)
		}
		if sha == ev.ArchiveSHA256 {
			return nil
		}
	}
	if matchedPlatform {
		return fmt.Errorf("evidence archiveSHA256 %s does not match any manifest download sha256 for %s/%s", ev.ArchiveSHA256, ev.OS, ev.Arch)
	}
	return fmt.Errorf("evidence archiveSHA256 %s has no matching manifest download for %s/%s", ev.ArchiveSHA256, ev.OS, ev.Arch)
}

func upsertCompatibilityRecord(index *PluginVersionIndex, rec PluginVersionRecord) {
	for i := range index.Versions {
		if index.Versions[i].Version == rec.Version {
			index.Versions[i] = rec
			return
		}
	}
	index.Versions = append(index.Versions, rec)
}

func sortCompatibilityIndex(index *PluginVersionIndex) {
	slices.SortFunc(index.Versions, func(a, b PluginVersionRecord) int {
		return -semver.Compare(a.Version, b.Version)
	})
	for i := range index.Versions {
		sortCompatibilityEvidence(index.Versions[i].Compatibility)
	}
}

func sortCompatibilityEvidence(evidence []PluginCompatibilityEvidence) {
	slices.SortFunc(evidence, func(a, b PluginCompatibilityEvidence) int {
		if c := semver.Compare(a.EngineVersion, b.EngineVersion); c != 0 {
			return c
		}
		if c := strings.Compare(a.Mode, b.Mode); c != 0 {
			return c
		}
		if c := strings.Compare(a.OS, b.OS); c != 0 {
			return c
		}
		if c := strings.Compare(a.Arch, b.Arch); c != 0 {
			return c
		}
		return strings.Compare(a.Status, b.Status)
	})
}

func dedupeCompatibilityEvidence(evidence []PluginCompatibilityEvidence) []PluginCompatibilityEvidence {
	out := make([]PluginCompatibilityEvidence, 0, len(evidence))
	seen := map[string]bool{}
	for i := range evidence {
		ev := &evidence[i]
		key := strings.Join([]string{
			ev.Plugin, ev.Version, ev.EngineVersion, ev.Mode, ev.Status, ev.OS, ev.Arch, ev.ArchiveSHA256,
		}, "\x00")
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, *ev)
	}
	return out
}

func deriveCompatibilityRanges(index *PluginVersionIndex) {
	for recIdx := range index.Versions {
		rec := &index.Versions[recIdx]
		byKey := map[string][]PluginCompatibilityEvidence{}
		hasFail := map[string]bool{}
		for i := range rec.Compatibility {
			ev := &rec.Compatibility[i]
			if ev.CompatibleEngineRange != nil {
				continue
			}
			key := strings.Join([]string{ev.Mode, ev.OS, ev.Arch, ev.ArchiveSHA256}, "\x00")
			if ev.Status == PluginCompatibilityStatusFail {
				hasFail[key] = true
				continue
			}
			if ev.Status == PluginCompatibilityStatusPass {
				byKey[key] = append(byKey[key], *ev)
			}
		}
		for key, passes := range byKey {
			if hasFail[key] || len(passes) < 2 {
				continue
			}
			sortCompatibilityEvidence(passes)
			first, last := passes[0], passes[len(passes)-1]
			if first.EngineVersion == last.EngineVersion {
				continue
			}
			rangeEvidence := last
			rangeEvidence.CompatibleEngineRange = &PluginCompatibilityRange{
				Min:        first.EngineVersion,
				Max:        last.EngineVersion,
				Derivation: "enumerated-pass",
			}
			normalized, err := ValidateCompatibilityEvidence(rangeEvidence)
			if err == nil {
				rec.Compatibility = append(rec.Compatibility, normalized)
			}
		}
	}
}

func compatibilityIndexIsStale(index *PluginVersionIndex, latestEngine string) bool {
	newest := ""
	for recIdx := range index.Versions {
		rec := &index.Versions[recIdx]
		for evIdx := range rec.Compatibility {
			ev := &rec.Compatibility[evIdx]
			if newest == "" || semver.Compare(ev.EngineVersion, newest) > 0 {
				newest = ev.EngineVersion
			}
		}
	}
	return newest == "" || semver.Compare(newest, latestEngine) < 0
}

// manifestAdvertisesIaCProvider returns true when a registry manifest declares
// an iacProvider capability with a non-empty provider name. These plugins must
// supply typed-iac conformance evidence; legacy-host-load evidence is rejected
// at index-update time for such manifests.
func manifestAdvertisesIaCProvider(m *RegistryManifest) bool {
	return m != nil &&
		m.Capabilities != nil &&
		m.Capabilities.IaCProvider != nil &&
		m.Capabilities.IaCProvider.Name != ""
}

func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-index-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) //nolint:errcheck
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	if dirFile, err := os.Open(dir); err == nil {
		_ = dirFile.Sync()
		_ = dirFile.Close()
	}
	return nil
}
