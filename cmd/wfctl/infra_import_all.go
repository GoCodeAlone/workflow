package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/dns/record"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// runInfraImportAll implements `wfctl infra import-all` — a bulk wrapper that
// resolves a single iac.provider module from the config, runs the provider's
// IaCProviderEnumerator.EnumerateAll(resourceType), and then iterates
// IaCProvider.Import for each enumerated cloud ID, persisting each
// synthesized ResourceState into the configured iac.state backend.
//
// Per-zone failure isolation: a single Import failure does NOT abort the
// loop; failures are accumulated and surfaced as a single error at the end.
// The caller observes a non-zero exit when any zone failed, with the
// failure list in the error message — matching the design's Phase 2
// "non-zero exit if any zone import fails" contract.
//
// `--provider` is the iac.provider MODULE NAME from the config file (e.g.
// "do-prod"), NOT the plugin type discriminator (e.g. "digitalocean"). The
// helper resolveProviderModuleByName walks cfg.Modules to extract the plugin
// type from modCfg["provider"] — same pattern as resolveProviderForSpec for
// the single-resource import path.
//
// `--type` is the resource-type string the EnumerateAll method accepts —
// initially "infra.dns"; the EnumeratorAll contract is type-agnostic so this
// command works for any resource type a provider plugin implements
// (spaces_key, dns, etc.).
//
// `--dry-run` probes each enumerated cloud ID via provider.Import to surface
// auth + reachability failures without persisting any state. Useful for
// dispatch-readiness checks before running the actual import.
//
// `--output` / `-o` dumps the post-import state-store contents to the given
// file as a JSON array, for scenario test harnesses that need to diff state
// across runs without re-reading the live state backend.
func runInfraImportAll(args []string) error {
	fs := flag.NewFlagSet("infra import-all", flag.ContinueOnError)
	var configFile, envName, providerName, resourceType, pluginDirFlag, outputPath string
	var dryRun bool
	var format string
	var sanitize bool
	fs.StringVar(&configFile, "config", "", "Config file")
	fs.StringVar(&configFile, "c", "", "Config file (short for --config)")
	fs.StringVar(&envName, "env", "", "Environment name")
	fs.StringVar(&providerName, "provider", "", "Provider module name from config (required)")
	fs.StringVar(&resourceType, "type", "", "Resource type to enumerate, e.g. infra.dns (required)")
	fs.BoolVar(&dryRun, "dry-run", false, "List enumerated resources without persisting state")
	fs.StringVar(&pluginDirFlag, "plugin-dir", "", "Plugin directory (overrides WFCTL_PLUGIN_DIR and default data/plugins)")
	fs.StringVar(&outputPath, "output", "", "Optional: dump state-store contents to this file (in addition to the state backend)")
	fs.StringVar(&outputPath, "o", "", "Output path (short for --output)")
	fs.StringVar(&format, "format", "state", "Output format for --output: state|portfolio")
	fs.BoolVar(&sanitize, "sanitize", false, "Portfolio only: redact TXT secrets + example-ize public IPs")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if providerName == "" {
		return fmt.Errorf("import-all requires --provider (the iac.provider module name from the config)")
	}
	if resourceType == "" {
		return fmt.Errorf("import-all requires --type (e.g. infra.dns)")
	}
	if format != "state" && format != "portfolio" {
		return fmt.Errorf("import-all: --format %q is not valid; must be state or portfolio", format)
	}
	if sanitize && format != "portfolio" {
		return fmt.Errorf("import-all: --sanitize requires --format portfolio")
	}

	// Plugin-dir flag follows the same scoped-override pattern used by
	// runInfraImport: temporarily set the package-level
	// currentInfraPluginDir so downstream provider resolution honors the
	// flag, then restore on exit. Empty flag → use existing default.
	prevInfraPluginDir := currentInfraPluginDir
	currentInfraPluginDir = pluginDirFlag
	defer func() { currentInfraPluginDir = prevInfraPluginDir }()

	cfgFile, err := resolveInfraConfig(fs, configFile)
	if err != nil {
		return err
	}

	ctx := context.Background()
	providerType, providerCfg, err := resolveProviderModuleByName(cfgFile, envName, providerName)
	if err != nil {
		return err
	}
	provider, closer, err := resolveIaCProvider(ctx, providerType, providerCfg)
	if err != nil {
		return fmt.Errorf("load provider %q: %w", providerType, err)
	}
	if closer != nil {
		defer func() {
			if cerr := closer.Close(); cerr != nil {
				fmt.Fprintf(os.Stderr, "warning: provider %q shutdown: %v\n", providerType, cerr)
			}
		}()
	}

	store, err := resolveStateStore(cfgFile, envName)
	if err != nil {
		return fmt.Errorf("resolve state store: %w", err)
	}
	// Note: unlike runInfraImport, dry-run does NOT require a writable
	// state backend — the dry-run path probes via provider.Import without
	// calling store.SaveResource. The noopStateStore is acceptable for
	// dry-run. For the real import path, require a writable backend.
	if !dryRun && isNoopStateStore(store) {
		return fmt.Errorf("infra import-all requires a writable iac.state backend; add an iac.state module before importing")
	}

	n, dispatchErr := runInfraImportAllWithDeps(ctx, provider, providerType, store, resourceType, dryRun)
	if outputPath != "" {
		var werr error
		if format == "portfolio" {
			werr = dumpPortfolioToFile(ctx, store, outputPath, sanitize)
		} else {
			werr = dumpStateToFile(ctx, store, outputPath)
		}
		if werr != nil {
			// Output dump is auxiliary; surface as a warning rather than
			// overwriting the dispatch error. Operators care about the
			// import result first; the dump is a debug-trail bonus.
			fmt.Fprintf(os.Stderr, "warning: --output dump to %q failed: %v\n", outputPath, werr)
		}
	}
	if dryRun {
		fmt.Printf("dry-run: %d %s zones would be imported via provider %q\n", n, resourceType, providerName)
	} else {
		fmt.Printf("imported %d %s zones via provider %q\n", n, resourceType, providerName)
	}
	return dispatchErr
}

// runInfraImportAllWithDeps is the testable dispatch core. Split from
// runInfraImportAll so unit tests can drive it with stubbed
// IaCProvider + infraStateStore implementations without touching plugin
// discovery, env resolution, or the filesystem.
//
// Contract:
//   - provider MUST implement interfaces.EnumeratorAll (the
//     IaCProviderEnumerator strict-contract sub-interface). If not, returns
//     (0, error) immediately — operators see a clear "provider does not
//     support EnumerateAll" message rather than a panic or empty result.
//   - Per-zone failures are isolated. Each enumerated cloud ID is imported
//     independently; a failure on zone N does not block zone N+1. The total
//     count of *successful* imports is returned alongside an error
//     summarizing all failures (or nil if zero failures).
//   - dryRun=true probes each cloud ID via provider.Import but does NOT
//     call store.SaveResource. The import-state result is discarded; only
//     the success/failure tier of the call matters. This validates auth +
//     reachability without persisting state.
func runInfraImportAllWithDeps(ctx context.Context, provider interfaces.IaCProvider, providerType string, store infraStateStore, resourceType string, dryRun bool) (int, error) {
	enumerator, ok := provider.(interfaces.EnumeratorAll)
	if !ok {
		return 0, fmt.Errorf("provider %q does not implement EnumerateAll (interfaces.EnumeratorAll); cannot bulk-import %s", providerType, resourceType)
	}
	outputs, err := enumerator.EnumerateAll(ctx, resourceType)
	if err != nil {
		return 0, fmt.Errorf("enumerate %s via %s: %w", resourceType, providerType, err)
	}
	imported := 0
	var failures []string
	for _, o := range outputs {
		if o == nil {
			continue
		}
		zoneName, _ := o.Outputs["zone"].(string)
		if zoneName == "" {
			zoneName = o.ProviderID
		}
		if zoneName == "" {
			// Skip entries with neither Outputs.zone nor ProviderID — a
			// well-formed EnumerateAll output always has at least one;
			// surface as a soft failure rather than a hard abort so a
			// single malformed provider entry doesn't tank the run.
			failures = append(failures, "(unnamed): EnumerateAll output has empty zone + empty ProviderID; skipped")
			continue
		}
		if dryRun {
			if _, perr := provider.Import(ctx, o.ProviderID, resourceType); perr != nil {
				failures = append(failures, fmt.Sprintf("%s: dry-run probe failed: %v", zoneName, perr))
				continue
			}
			fmt.Printf("would import: zone=%s id=%s\n", zoneName, o.ProviderID)
			imported++
			continue
		}
		imported_state, ierr := provider.Import(ctx, o.ProviderID, resourceType)
		if ierr != nil {
			failures = append(failures, fmt.Sprintf("%s: import: %v", zoneName, ierr))
			continue
		}
		synth, serr := buildResourceStateFromImport(zoneName, o.ProviderID, resourceType, providerType, imported_state)
		if serr != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", zoneName, serr))
			continue
		}
		if perr := store.SaveResource(ctx, synth); perr != nil {
			failures = append(failures, fmt.Sprintf("%s: save: %v", zoneName, perr))
			continue
		}
		imported++
	}
	if len(failures) > 0 {
		return imported, fmt.Errorf("import-all completed with %d failure(s):\n  %s", len(failures), strings.Join(failures, "\n  "))
	}
	return imported, nil
}

// buildResourceStateFromImport synthesizes an interfaces.ResourceState from
// an EnumerateAll output paired with the provider's Import result. The
// ResourceSpec is fabricated from the zone name (the user-facing identifier)
// because import-all has no per-zone spec in the config — the operator is
// importing zones that may not yet be declared.
//
// Reuses resourceStateFromImportedState (workflow/cmd/wfctl/infra.go:1198)
// for ProviderID resolution + AppliedConfig hashing + timestamp normalization
// so the synthesized state matches the single-resource import path exactly.
func buildResourceStateFromImport(zoneName, cloudID, resourceType, providerType string, imported *interfaces.ResourceState) (interfaces.ResourceState, error) {
	// Prefix the sanitized zone name with the resource type so that importing
	// two different types (e.g. infra.dns and infra.dns_delegation) for the
	// same domain produces DISTINCT IDs and therefore distinct on-disk
	// filenames.  sanitizeStateID maps "/" → "_", so
	//   "infra.dns/example-com"           → infra.dns_example-com.json
	//   "infra.dns_delegation/example-com" → infra.dns_delegation_example-com.json
	// ProviderID stays as the bare cloudID (domain) so record.FromResourceStates
	// can group both types into a single portfolio snapshot by (Provider, Domain).
	spec := interfaces.ResourceSpec{
		Name: resourceType + "/" + sanitizeImportedZoneName(zoneName),
		Type: resourceType,
	}
	state, err := resourceStateFromImportedState(spec, providerType, imported, cloudID)
	if err != nil {
		return interfaces.ResourceState{}, err
	}
	if cloudID != "" {
		state.ProviderID = cloudID
	}
	return state, nil
}

// sanitizeImportedZoneName converts a zone identifier (typically a FQDN like
// "example.test") into a name suitable for use as ResourceState.ID. Dots and
// other characters that are valid in a domain but problematic in YAML keys
// + filesystem-state paths are replaced with hyphens. Idempotent: an already
// sanitized input passes through unchanged.
func sanitizeImportedZoneName(zone string) string {
	if zone == "" {
		return zone
	}
	// Mirror existing slug conventions in the codebase (alphanumerics,
	// hyphens, underscores). Replace anything else with hyphen so the
	// result is filesystem + YAML safe across all state backends.
	out := make([]byte, 0, len(zone))
	for i := 0; i < len(zone); i++ {
		c := zone[i]
		switch {
		case c >= 'a' && c <= 'z',
			c >= 'A' && c <= 'Z',
			c >= '0' && c <= '9',
			c == '-', c == '_':
			out = append(out, c)
		default:
			out = append(out, '-')
		}
	}
	return string(out)
}

// resolveProviderModuleByName resolves an iac.provider module by name from
// the config file, returning (a) the plugin discriminator (from
// modCfg["provider"]) used to dispatch to a concrete provider plugin and
// (b) the fully env-resolved module config.
//
// Mirrors resolveProviderForSpec (workflow/cmd/wfctl/infra.go:1157) but
// indexes the modules slice by NAME rather than by an iac_provider/provider
// reference on a ResourceSpec. The bulk-import wrapper has no spec; the
// operator picks the provider module directly via --provider.
//
// Implementation pinned to mirror resolveProviderForSpec exactly (cycle-4
// adversarial finding I2):
//   - Range over INDEX (`for i := range cfg.Modules` + `m := &cfg.Modules[i]`)
//     because m.ResolveForEnv has a pointer receiver.
//   - m.ResolveForEnv returns (*config.ResolvedModule, bool) — guard via
//     `if !ok` rather than `err != nil`.
//   - config.ExpandEnvInMapPreservingKeys returns a single
//     map[string]any — not (map, error). Single-value assignment.
func resolveProviderModuleByName(cfgFile, envName, name string) (string, map[string]any, error) {
	cfg, err := config.LoadFromFile(cfgFile)
	if err != nil {
		return "", nil, fmt.Errorf("load %s: %w", cfgFile, err)
	}
	for i := range cfg.Modules {
		m := &cfg.Modules[i]
		if m.Type != "iac.provider" || m.Name != name {
			continue
		}
		var modCfg map[string]any
		if envName != "" {
			resolved, ok := m.ResolveForEnv(envName)
			if !ok {
				return "", nil, fmt.Errorf("provider module %q is disabled for environment %q", name, envName)
			}
			modCfg = config.ExpandEnvInMapPreservingKeys(resolved.Config, infraPreserveKeys)
		} else {
			modCfg = config.ExpandEnvInMapPreservingKeys(m.Config, infraPreserveKeys)
		}
		providerType, _ := modCfg["provider"].(string)
		if providerType == "" {
			return "", nil, fmt.Errorf("provider module %q has no 'provider' type configured", name)
		}
		return providerType, modCfg, nil
	}
	return "", nil, fmt.Errorf("no iac.provider module named %q in config", name)
}

// dumpStateToFile snapshots the current state-store contents to outputPath
// as a JSON object with a "resources" array. Intended for scenario test
// harnesses that diff state across runs without re-reading the live state
// backend (which may be remote or expensive to query).
//
// The 0o600 file mode mirrors the sister state-emit paths (configHashMap,
// fsWfctlStateStore) so secrets in AppliedConfig/Outputs are not
// world-readable. Operators wiring this into CI should treat the output as
// sensitive.
func dumpStateToFile(ctx context.Context, store infraStateStore, path string) error {
	resources, err := store.ListResources(ctx)
	if err != nil {
		return fmt.Errorf("list resources: %w", err)
	}
	data, err := json.MarshalIndent(map[string]any{"resources": resources}, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// dumpPortfolioToFile converts the state-store contents to a canonical
// dns-portfolio export and writes it to path as JSON.
//
// This is an auxiliary dump (mirroring dumpStateToFile's auxiliary role):
// errors are surfaced as warnings by the caller and do not change the
// command exit code — the import + state persistence already succeeded.
//
// The portfolio is validated (structural only — unknown record types are
// preserved, per the design's open-set snapshot contract) before writing.
// If sanitize is true, TXT secrets + public IPs are redacted so the file
// can be committed to a public repository.
func dumpPortfolioToFile(ctx context.Context, store infraStateStore, path string, sanitize bool) error {
	states, err := store.ListResources(ctx)
	if err != nil {
		return fmt.Errorf("list resources: %w", err)
	}
	p := record.FromResourceStates(states)
	if sanitize {
		record.Sanitize(&p)
	}
	if err := p.Validate(); err != nil {
		return fmt.Errorf("portfolio validate: %w", err)
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
