package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/plugin"
)

type installedIaCProvider struct {
	directory  string
	capability plugin.IaCProviderCapability
}

type installedIaCProviders struct {
	owners     map[string]installedIaCProvider
	types      map[string]struct{}
	duplicates map[string]error
	issues     []installedIaCManifestIssue
}

type installedIaCManifestIssue struct {
	directory string
	owners    []string
	err       error
}

// Check raw ownership before PluginManifest merges legacy resourceTypes. Both
// discovery and executable selection must see the same bytes and owner.
func decodeInstalledIaCManifest(data []byte, directory string) (*plugin.PluginManifest, []string, error) {
	type owner struct {
		Name string `json:"name"`
	}
	var raw struct {
		IaCProvider  json.RawMessage `json:"iacProvider"`
		Capabilities json.RawMessage `json:"capabilities"`
	}
	invalid := func() error { return fmt.Errorf("plugin %q has invalid IaC manifest metadata", directory) }
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, nil, invalid()
	}
	var legacy struct {
		IaCProvider json.RawMessage `json:"iacProvider"`
	}
	if caps := bytes.TrimSpace(raw.Capabilities); len(caps) > 0 && caps[0] == '{' {
		if err := json.Unmarshal(caps, &legacy); err != nil {
			return nil, nil, invalid()
		}
	}
	var owners []string
	var ownerErr error
	// Keep both typed owner hints even if one declaration is damaged. They
	// diagnose needed metadata; only a fully normalized manifest authorizes.
	for _, declaration := range []json.RawMessage{raw.IaCProvider, legacy.IaCProvider} {
		if len(declaration) == 0 {
			continue
		}
		var declared owner
		if err := json.Unmarshal(declaration, &declared); err != nil {
			ownerErr = invalid()
		}
		if declared.Name != "" {
			owners = append(owners, declared.Name)
		}
	}
	if len(owners) == 2 && owners[0] != owners[1] {
		return nil, owners, fmt.Errorf("plugin %q has conflicting IaC provider owners", directory)
	}
	if ownerErr != nil {
		return nil, owners, ownerErr
	}
	var manifest plugin.PluginManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, owners, invalid()
	}
	for _, typ := range manifest.IaCProvider.ResourceTypes {
		if typ == "" || strings.TrimSpace(typ) != typ {
			return nil, owners, invalid()
		}
	}
	return &manifest, owners, nil
}

func scanInstalledIaCProviders(root string) (*installedIaCProviders, error) {
	catalog := &installedIaCProviders{
		owners:     map[string]installedIaCProvider{},
		types:      map[string]struct{}{},
		duplicates: map[string]error{},
	}
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return catalog, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan plugin directory %q: %w", root, err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, entry.Name(), "plugin.json"))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			catalog.issues = append(catalog.issues, installedIaCManifestIssue{
				directory: entry.Name(), err: fmt.Errorf("read plugin %q manifest: %w", entry.Name(), err),
			})
			continue
		}
		manifest, owners, err := decodeInstalledIaCManifest(data, entry.Name())
		if err != nil {
			catalog.issues = append(catalog.issues, installedIaCManifestIssue{directory: entry.Name(), owners: owners, err: err})
			continue
		}
		capability := manifest.IaCProvider
		if capability.Name == "" {
			continue
		}
		if _, exists := catalog.owners[capability.Name]; exists {
			catalog.duplicates[capability.Name] = fmt.Errorf("multiple installed plugins declare IaC provider %q", capability.Name)
		} else {
			catalog.owners[capability.Name] = installedIaCProvider{directory: entry.Name(), capability: capability}
		}
		for _, typ := range capability.ResourceTypes {
			catalog.types[typ] = struct{}{}
		}
	}
	return catalog, nil
}

func (c *installedIaCProviders) selectOwner(name string) (installedIaCProvider, bool, error) {
	if err := c.duplicates[name]; err != nil {
		return installedIaCProvider{}, false, err
	}
	for _, issue := range c.issues {
		needed := issue.directory == name
		for _, owner := range issue.owners {
			needed = needed || owner == name
		}
		if needed {
			return installedIaCProvider{}, false, issue.err
		}
	}
	selected, found := c.owners[name]
	if !found {
		// Unknown damaged ownership cannot supply a missing selection. Retain
		// its error instead of returning a misleading successful lookup.
		for _, issue := range c.issues {
			if len(issue.owners) == 0 {
				return installedIaCProvider{}, false, issue.err
			}
		}
	}
	return selected, found, nil
}

func infraPluginDirectory() string {
	if currentInfraPluginDir != "" {
		return currentInfraPluginDir
	}
	if root := os.Getenv("WFCTL_PLUGIN_DIR"); root != "" {
		return root
	}
	return "./data/plugins"
}

type iaCResourceDiscovery struct {
	defs       map[string]providerDef
	disabled   map[string]struct{}
	defaultRef string
	root       string
	catalog    *installedIaCProviders
}

func newIaCResourceDiscovery(cfg *config.WorkflowConfig, envName string) (*iaCResourceDiscovery, error) {
	names := map[string]struct{}{}
	for _, mod := range cfg.Modules {
		if mod.Type != "iac.provider" {
			continue
		}
		if _, exists := names[mod.Name]; exists {
			return nil, fmt.Errorf("duplicate iac.provider module %q", mod.Name)
		}
		names[mod.Name] = struct{}{}
	}
	defs, _, disabled := resolveProviderDefs(cfg, envName)
	d := &iaCResourceDiscovery{defs: defs, disabled: disabled, root: infraPluginDirectory()}
	if envName != "" && cfg.Environments[envName] != nil {
		d.defaultRef = os.ExpandEnv(cfg.Environments[envName].Provider)
	}
	return d, nil
}

func (d *iaCResourceDiscovery) classify(mod *config.ResolvedModule) (bool, error) {
	if isInfraType(mod.Type) {
		return true, nil
	}
	if mod.Type == "iac.provider" || mod.Type == "iac.state" {
		return false, nil
	}
	// Resolve routing with the same expansion as provider dispatch, without
	// expanding payloads or changing the raw config used by input scanning.
	routing := map[string]any{}
	for _, key := range []string{"iac_provider", "provider"} {
		if value, ok := mod.Config[key].(string); ok {
			routing[key] = config.ExpandEnvInValue(value)
		}
	}
	ref := resolveIaCProviderRef(routing)
	_, explicit := mod.Config["iac_provider"]
	_, declared := d.defs[ref]
	_, disabled := d.disabled[ref]
	defaultDef, defaultDeclared := d.defs[d.defaultRef]
	if d.catalog == nil {
		catalog, err := scanInstalledIaCProviders(d.root)
		if err != nil {
			if !explicit && !declared && !disabled && (ref != "" || !defaultDeclared) {
				return false, nil
			}
			return false, err
		}
		d.catalog = catalog
	}
	if ref == "" && defaultDeclared {
		if defaultDef.provType == "" {
			return false, fmt.Errorf("IaC resource %q references unconfigured default provider %q", mod.Name, d.defaultRef)
		}
		// Missing default-owner metadata must not turn a previously declared
		// custom resource into an ordinary module and an omission Delete.
		_, found, err := d.catalog.selectOwner(defaultDef.provType)
		if err != nil {
			return false, err
		}
		if !found {
			return false, fmt.Errorf("IaC resource %q: no installed plugin declares selected default provider %q", mod.Name, defaultDef.provType)
		}
	}
	_, advertised := d.catalog.types[mod.Type]
	if !explicit && !advertised && !declared && !disabled {
		return false, nil
	}
	if explicit {
		value, ok := mod.Config["iac_provider"].(string)
		if !ok || value == "" {
			return false, fmt.Errorf("IaC resource %q requires a nonempty string iac_provider reference", mod.Name)
		}
	}
	if ref == "" {
		ref = d.defaultRef
	}
	if ref == "" {
		return false, fmt.Errorf("IaC resource %q (%s) requires an explicit provider reference", mod.Name, mod.Type)
	}
	if _, disabled := d.disabled[ref]; disabled {
		return false, fmt.Errorf("IaC resource %q references disabled provider %q", mod.Name, ref)
	}
	def, ok := d.defs[ref]
	if !ok || def.provType == "" {
		return false, fmt.Errorf("IaC resource %q references undeclared or unconfigured provider %q", mod.Name, ref)
	}
	selected, ok, err := d.catalog.selectOwner(def.provType)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, fmt.Errorf("IaC resource %q: no installed plugin declares selected provider %q", mod.Name, def.provType)
	}
	for _, typ := range selected.capability.ResourceTypes {
		if typ == mod.Type {
			return true, nil
		}
	}
	return false, fmt.Errorf("IaC resource %q type %q is not declared by selected provider %q", mod.Name, mod.Type, def.provType)
}

func resolveDiscoveryModule(mod *config.ModuleConfig, envName string) (*config.ResolvedModule, bool) {
	if envName != "" {
		return mod.ResolveForEnv(envName)
	}
	return &config.ResolvedModule{
		Name: mod.Name, Type: mod.Type, Protected: mod.Protected, Config: cloneMap(mod.Config),
	}, true
}

func hasDirectIaCResources(cfgFile, envName string) (bool, error) {
	specs, err := parseInfraResourceSpecsForEnv(cfgFile, envName)
	if err != nil {
		return false, err
	}
	for _, spec := range specs {
		if !strings.HasPrefix(spec.Type, "platform.") {
			return true, nil
		}
	}
	return false, nil
}
