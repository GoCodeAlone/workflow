package inventory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	workflowplugin "github.com/GoCodeAlone/workflow/plugin"
)

// EcosystemOptions controls ecosystem capability inventory generation.
type EcosystemOptions struct {
	RegistryDir     string
	RepoRoot        string
	TaxonomyPath    string
	GeneratedAt     time.Time
	WorkflowVersion string
}

// CollectEcosystem reads registry and local plugin manifests into a capability inventory.
func CollectEcosystem(opts EcosystemOptions) (*Inventory, error) {
	tax, err := LoadTaxonomy(opts.TaxonomyPath)
	if err != nil {
		return nil, err
	}
	generatedAt := opts.GeneratedAt
	if generatedAt.IsZero() {
		generatedAt = time.Now().UTC()
	}
	builder := newInventoryBuilder(tax)
	released, err := collectRegistryProviders(builder, opts.RegistryDir)
	if err != nil {
		return nil, err
	}
	local, err := collectLocalProviders(builder, opts.RepoRoot)
	if err != nil {
		return nil, err
	}

	inv := builder.inventory()
	inv.Metadata = Metadata{
		Generator:       "wfctl capability ecosystem",
		GeneratedAt:     generatedAt.UTC().Format(time.RFC3339),
		WorkflowVersion: opts.WorkflowVersion,
		TaxonomyVersion: tax.Version,
		TaxonomyDigest:  tax.Digest(),
		RegistrySource:  opts.RegistryDir,
		LocalRepoRoot:   opts.RepoRoot,
		Counts: map[string]int{
			"capabilities":      len(inv.Capabilities),
			"findings":          len(inv.Findings),
			"releasedProviders": released,
			"localProviders":    local,
		},
	}
	return inv, nil
}

type registryManifest struct {
	Name         string                `json:"name"`
	Version      string                `json:"version"`
	Description  string                `json:"description"`
	Type         string                `json:"type"`
	Status       string                `json:"status"`
	Repository   string                `json:"repository"`
	Capabilities *registryCapabilities `json:"capabilities"`
}

type registryCapabilities struct {
	ModuleTypes      []string `json:"moduleTypes"`
	StepTypes        []string `json:"stepTypes"`
	TriggerTypes     []string `json:"triggerTypes"`
	WorkflowHandlers []string `json:"workflowHandlers"`
	WiringHooks      []string `json:"wiringHooks"`
	IaCServices      []string `json:"iacServices"`
	IaCStateBackends []string `json:"iacStateBackends"`
}

func collectRegistryProviders(builder *inventoryBuilder, registryDir string) (int, error) {
	if strings.TrimSpace(registryDir) == "" {
		return 0, nil
	}
	pluginsDir := filepath.Join(registryDir, "plugins")
	if st, err := os.Stat(pluginsDir); err == nil && st.IsDir() {
		registryDir = pluginsDir
	}
	entries, err := os.ReadDir(registryDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("read registry dir: %w", err)
	}
	count := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(registryDir, entry.Name(), "manifest.json")
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return count, fmt.Errorf("read registry manifest %s: %w", path, err)
		}
		var manifest registryManifest
		if err := json.Unmarshal(data, &manifest); err != nil {
			return count, fmt.Errorf("parse registry manifest %s: %w", path, err)
		}
		if manifest.Name == "" {
			manifest.Name = entry.Name()
		}
		builder.addProvider(providerInput{
			Name:          manifest.Name,
			Kind:          firstNonEmpty(manifest.Type, "registry"),
			EvidenceKind:  "registry-manifest",
			Version:       manifest.Version,
			ReleaseStatus: firstNonEmpty(manifest.Status, "released"),
			Source:        firstNonEmpty(manifest.Repository, path),
			Path:          path,
			Raw:           registryRawCapabilities(manifest.Capabilities),
		})
		count++
	}
	return count, nil
}

func collectLocalProviders(builder *inventoryBuilder, repoRoot string) (int, error) {
	if strings.TrimSpace(repoRoot) == "" {
		return 0, nil
	}
	entries, err := os.ReadDir(repoRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("read local repo root: %w", err)
	}
	count := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, "workflow-plugin-") {
			continue
		}
		manifestPath := filepath.Join(repoRoot, name, "plugin.json")
		if _, err := os.Stat(manifestPath); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return count, fmt.Errorf("stat local plugin manifest %s: %w", manifestPath, err)
		}
		manifest, err := workflowplugin.LoadManifest(manifestPath)
		if err != nil {
			return count, fmt.Errorf("load local plugin manifest %s: %w", manifestPath, err)
		}
		builder.addProvider(providerInput{
			Name:          firstNonEmpty(manifest.Name, name),
			Kind:          "local-plugin",
			EvidenceKind:  "plugin-manifest",
			Version:       manifest.Version,
			ReleaseStatus: "local-only",
			Source:        firstNonEmpty(manifest.Repository, manifestPath),
			Path:          manifestPath,
			Raw:           manifestRawCapabilities(manifest),
		})
		count++
	}
	return count, nil
}

type rawCapability struct {
	Kind     string
	Value    string
	JSONPath string
	Detail   string
}

func registryRawCapabilities(caps *registryCapabilities) []rawCapability {
	if caps == nil {
		return nil
	}
	var raw []rawCapability
	raw = appendRaw(raw, "module", "capabilities.moduleTypes", "moduleTypes", caps.ModuleTypes)
	raw = appendRaw(raw, "step", "capabilities.stepTypes", "stepTypes", caps.StepTypes)
	raw = appendRaw(raw, "trigger", "capabilities.triggerTypes", "triggerTypes", caps.TriggerTypes)
	raw = appendRaw(raw, "workflow", "capabilities.workflowHandlers", "workflowHandlers", caps.WorkflowHandlers)
	raw = appendRaw(raw, "wiringHook", "capabilities.wiringHooks", "wiringHooks", caps.WiringHooks)
	raw = appendRaw(raw, "iacService", "capabilities.iacServices", "iacServices", caps.IaCServices)
	raw = appendRaw(raw, "iacStateBackend", "capabilities.iacStateBackends", "iacStateBackends", caps.IaCStateBackends)
	return raw
}

func manifestRawCapabilities(manifest *workflowplugin.PluginManifest) []rawCapability {
	if manifest == nil {
		return nil
	}
	var raw []rawCapability
	raw = appendRaw(raw, "module", "moduleTypes", "moduleTypes", manifest.ModuleTypes)
	raw = appendRaw(raw, "step", "stepTypes", "stepTypes", manifest.StepTypes)
	raw = appendRaw(raw, "trigger", "triggerTypes", "triggerTypes", manifest.TriggerTypes)
	raw = appendRaw(raw, "workflow", "workflowTypes", "workflowTypes", manifest.WorkflowTypes)
	raw = appendRaw(raw, "wiringHook", "wiringHooks", "wiringHooks", manifest.WiringHooks)
	raw = appendRaw(raw, "iacService", "iacServices", "iacServices", manifest.IaCServices)
	raw = appendRaw(raw, "iacStateBackend", "iacStateBackends", "iacStateBackends", manifest.IaCStateBackends)
	if strings.TrimSpace(manifest.IaCProvider.Name) != "" {
		raw = append(raw, rawCapability{
			Kind:     "provider",
			Value:    manifest.IaCProvider.Name,
			JSONPath: "iacProvider.name",
			Detail:   "iacProvider.name",
		})
	}
	raw = appendRaw(raw, "module", "iacProvider.resourceTypes", "iacProvider.resourceTypes", manifest.IaCProvider.ResourceTypes)
	for i, cap := range manifest.Capabilities {
		if strings.TrimSpace(cap.Name) == "" {
			continue
		}
		raw = append(raw, rawCapability{
			Kind:     "provider",
			Value:    cap.Name,
			JSONPath: fmt.Sprintf("capabilities[%d].name", i),
			Detail:   fmt.Sprintf("capabilities[%d]", i),
		})
	}
	return raw
}

func appendRaw(raw []rawCapability, kind, jsonPrefix, detailPrefix string, values []string) []rawCapability {
	for i, value := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}
		raw = append(raw, rawCapability{
			Kind:     kind,
			Value:    value,
			JSONPath: fmt.Sprintf("%s[%d]", jsonPrefix, i),
			Detail:   fmt.Sprintf("%s[%d]", detailPrefix, i),
		})
	}
	return raw
}

type providerInput struct {
	Name          string
	Kind          string
	EvidenceKind  string
	Version       string
	ReleaseStatus string
	Source        string
	Path          string
	Raw           []rawCapability
}

type inventoryBuilder struct {
	tax      *Taxonomy
	caps     map[string]*Capability
	findings []Finding
}

func newInventoryBuilder(tax *Taxonomy) *inventoryBuilder {
	return &inventoryBuilder{
		tax:  tax,
		caps: make(map[string]*Capability),
	}
}

func (b *inventoryBuilder) addProvider(provider providerInput) {
	for _, raw := range provider.Raw {
		taxCap, ok := b.tax.MatchType(raw.Kind, raw.Value)
		if ok {
			b.addKnownCapability(provider, raw, taxCap)
			continue
		}
		b.addUnknownCapability(provider, raw)
	}
}

func (b *inventoryBuilder) addKnownCapability(provider providerInput, raw rawCapability, taxCap *TaxonomyCapability) {
	cap := b.ensureCapability(taxCap.ID, taxCap.Category, taxCap.Name, taxCap.Description, taxCap.Lifecycle, taxCap.Tags)
	evidence := evidenceFor(provider, raw)
	cap.Evidence = append(cap.Evidence, evidence)
	mergeProvider(cap, provider, raw)
}

func (b *inventoryBuilder) addUnknownCapability(provider providerInput, raw rawCapability) {
	id := "uncategorized:" + strings.ToLower(raw.Kind) + ":" + strings.ToLower(strings.TrimSpace(raw.Value))
	cap := b.ensureCapability(id, "uncategorized", raw.Value, "Raw capability declaration with no taxonomy mapping", "needs-review", nil)
	evidence := evidenceFor(provider, raw)
	cap.Evidence = append(cap.Evidence, evidence)
	mergeProvider(cap, provider, raw)
	finding := Finding{
		Level:        "warning",
		Code:         "needs-review",
		CapabilityID: id,
		Message:      fmt.Sprintf("raw %s capability %q is not mapped in the taxonomy", raw.Kind, raw.Value),
		Evidence:     []Evidence{evidence},
	}
	cap.Findings = append(cap.Findings, finding)
	b.findings = append(b.findings, finding)
}

func (b *inventoryBuilder) ensureCapability(id, category, name, description, lifecycle string, tags []string) *Capability {
	if cap, ok := b.caps[id]; ok {
		return cap
	}
	cap := &Capability{
		ID:          id,
		Category:    category,
		Name:        name,
		Description: description,
		Lifecycle:   lifecycle,
		Tags:        append([]string(nil), tags...),
	}
	b.caps[id] = cap
	return cap
}

func (b *inventoryBuilder) inventory() *Inventory {
	capabilities := make([]Capability, 0, len(b.caps))
	for _, cap := range b.caps {
		sort.Slice(cap.Providers, func(i, j int) bool {
			if cap.Providers[i].Name == cap.Providers[j].Name {
				return cap.Providers[i].ReleaseStatus < cap.Providers[j].ReleaseStatus
			}
			return cap.Providers[i].Name < cap.Providers[j].Name
		})
		sort.Slice(cap.Evidence, func(i, j int) bool {
			if cap.Evidence[i].SourcePath == cap.Evidence[j].SourcePath {
				return cap.Evidence[i].Detail < cap.Evidence[j].Detail
			}
			return cap.Evidence[i].SourcePath < cap.Evidence[j].SourcePath
		})
		capabilities = append(capabilities, *cap)
	}
	sort.Slice(capabilities, func(i, j int) bool {
		return capabilities[i].ID < capabilities[j].ID
	})
	sort.Slice(b.findings, func(i, j int) bool {
		if b.findings[i].CapabilityID == b.findings[j].CapabilityID {
			return b.findings[i].Message < b.findings[j].Message
		}
		return b.findings[i].CapabilityID < b.findings[j].CapabilityID
	})
	return &Inventory{
		Capabilities: capabilities,
		Findings:     append([]Finding(nil), b.findings...),
	}
}

func mergeProvider(cap *Capability, input providerInput, raw rawCapability) {
	rawName := raw.Kind + ":" + raw.Value
	for i := range cap.Providers {
		if cap.Providers[i].Name == input.Name && cap.Providers[i].ReleaseStatus == input.ReleaseStatus {
			if !containsString(cap.Providers[i].Capabilities, rawName) {
				cap.Providers[i].Capabilities = append(cap.Providers[i].Capabilities, rawName)
				sort.Strings(cap.Providers[i].Capabilities)
			}
			return
		}
	}
	cap.Providers = append(cap.Providers, Provider{
		Name:          input.Name,
		Kind:          input.Kind,
		Version:       input.Version,
		ReleaseStatus: input.ReleaseStatus,
		Source:        input.Source,
		Capabilities:  []string{rawName},
	})
}

func evidenceFor(provider providerInput, raw rawCapability) Evidence {
	sourceKind := provider.EvidenceKind
	if sourceKind == "" {
		sourceKind = provider.Kind
	}
	return Evidence{
		SourceKind: sourceKind,
		SourcePath: provider.Path,
		JSONPath:   raw.JSONPath,
		Detail:     raw.Detail,
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
