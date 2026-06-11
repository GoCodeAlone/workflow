package inventory

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/GoCodeAlone/workflow/config"
	workflowplugin "github.com/GoCodeAlone/workflow/plugin"
)

// AppOptions controls application capability profile generation.
type AppOptions struct {
	ManifestPath  string
	WorkflowPaths []string
	PluginDir     string
	LockfilePath  string
	TaxonomyPath  string
	GeneratedAt   time.Time
}

// CollectApp builds a capability profile for one application from Workflow-owned files.
func CollectApp(ctx context.Context, opts AppOptions) (*AppProfile, error) {
	tax, err := LoadTaxonomy(opts.TaxonomyPath)
	if err != nil {
		return nil, err
	}
	generatedAt := opts.GeneratedAt
	if generatedAt.IsZero() {
		generatedAt = time.Now().UTC()
	}
	builder := newAppBuilder(tax)

	providerIndex, providerCount, err := loadInstalledProviderIndex(opts.PluginDir)
	if err != nil {
		return nil, err
	}
	declaredPlugins, err := collectManifestUsage(builder, opts.ManifestPath)
	if err != nil {
		return nil, err
	}
	lockedPlugins, err := collectLockfileUsage(builder, opts.LockfilePath)
	if err != nil {
		return nil, err
	}
	workflowFiles, err := collectWorkflowUsage(ctx, builder, opts.WorkflowPaths, providerIndex)
	if err != nil {
		return nil, err
	}

	profile := builder.profile()
	profile.Metadata = Metadata{
		Generator:       "wfctl capability app",
		GeneratedAt:     generatedAt.UTC().Format(time.RFC3339),
		TaxonomyVersion: tax.Version,
		TaxonomyDigest:  tax.Digest(),
		Counts: map[string]int{
			"usage":              len(profile.Usage),
			"findings":           len(profile.Findings),
			"declaredPlugins":    declaredPlugins,
			"lockedPlugins":      lockedPlugins,
			"installedProviders": providerCount,
			"workflowFiles":      workflowFiles,
		},
	}
	return profile, nil
}

// CheckApp returns deterministic policy findings for an application profile.
func CheckApp(profile *AppProfile) []Finding {
	if profile == nil {
		return nil
	}
	findings := append([]Finding(nil), profile.Findings...)
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Code == findings[j].Code {
			return findings[i].CapabilityID < findings[j].CapabilityID
		}
		return findings[i].Code < findings[j].Code
	})
	return findings
}

func collectManifestUsage(builder *appBuilder, manifestPath string) (int, error) {
	if strings.TrimSpace(manifestPath) == "" {
		return 0, nil
	}
	manifest, err := config.LoadWfctlManifest(manifestPath)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	for i, pluginEntry := range manifest.Plugins {
		evidence := Evidence{
			SourceKind: "wfctl-manifest",
			SourcePath: manifestPath,
			JSONPath:   fmt.Sprintf("plugins[%d].name", i),
			Detail:     pluginEntry.Name,
		}
		builder.addRawUsage("plugin", pluginEntry.Name, "declared", "high", evidence, true)
	}
	return len(manifest.Plugins), nil
}

func collectLockfileUsage(builder *appBuilder, lockfilePath string) (int, error) {
	if strings.TrimSpace(lockfilePath) == "" {
		return 0, nil
	}
	lockfile, err := config.LoadWfctlLockfile(lockfilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	names := make([]string, 0, len(lockfile.Plugins))
	for name := range lockfile.Plugins {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		evidence := Evidence{
			SourceKind: "wfctl-lockfile",
			SourcePath: lockfilePath,
			JSONPath:   "plugins." + name,
			Detail:     name,
		}
		builder.addRawUsage("plugin", name, "declared", "high", evidence, true)
	}
	return len(names), nil
}

func collectWorkflowUsage(ctx context.Context, builder *appBuilder, workflowPaths []string, providerIndex map[string]struct{}) (int, error) {
	count := 0
	tenancyPresent := false
	var tenantPolicyFindings []Finding
	for _, workflowPath := range workflowPaths {
		if strings.TrimSpace(workflowPath) == "" {
			continue
		}
		cfg, err := config.NewFileSource(workflowPath).Load(ctx)
		if err != nil {
			return count, err
		}
		count++
		for i, module := range cfg.Modules {
			moduleCapabilityID := builder.capabilityIDForRaw("module", module.Type)
			evidence := Evidence{
				SourceKind: "workflow-config",
				SourcePath: workflowPath,
				JSONPath:   fmt.Sprintf("modules[%d].type", i),
				Detail:     module.Type,
			}
			builder.addRawUsage("module", module.Type, "declared", "high", evidence, providerIndexHas(providerIndex, "module", module.Type))
			moduleHasTenantEvidence := hasTenantEvidence(module.Config)
			if moduleHasTenantEvidence || strings.Contains(strings.ToLower(module.Type), "tenant") || moduleCapabilityID == "tenancy.scope" {
				tenancyPresent = true
				builder.addKnownUsage("tenancy.scope", "inferred", "medium", Evidence{
					SourceKind: "workflow-config",
					SourcePath: workflowPath,
					JSONPath:   fmt.Sprintf("modules[%d]", i),
					Detail:     module.Name,
				})
			}
			authCapabilityID := inferredAuthCapabilityID(module.Type)
			if authCapabilityID != "" {
				builder.addKnownUsage(authCapabilityID, "inferred", "medium", evidence)
			}
			if isDataModule(module.Type) && !moduleHasTenantEvidence {
				tenantPolicyFindings = append(tenantPolicyFindings, Finding{
					Level:        "warning",
					Code:         "tenant-evidence-missing",
					CapabilityID: moduleCapabilityID,
					Message:      fmt.Sprintf("module %q uses data storage without tenant evidence", module.Name),
					Evidence:     []Evidence{evidence},
				})
			}
		}
		if cfg.Secrets != nil || len(cfg.SecretStores) > 0 {
			builder.addKnownUsage("secrets.management", "inferred", "high", Evidence{
				SourceKind: "workflow-config",
				SourcePath: workflowPath,
				JSONPath:   "secrets",
				Detail:     "secrets or secretStores configured",
			})
		}
	}
	if tenancyPresent {
		for _, finding := range tenantPolicyFindings {
			builder.addFinding(finding)
		}
	}
	return count, nil
}

func loadInstalledProviderIndex(pluginDir string) (map[string]struct{}, int, error) {
	index := make(map[string]struct{})
	if strings.TrimSpace(pluginDir) == "" {
		return index, 0, nil
	}
	entries, err := os.ReadDir(pluginDir)
	if err != nil {
		if os.IsNotExist(err) {
			return index, 0, nil
		}
		return index, 0, fmt.Errorf("read plugin dir: %w", err)
	}
	count := 0
	for _, entry := range entries {
		manifestPath := filepath.Join(pluginDir, entry.Name(), "plugin.json")
		if !entry.IsDir() {
			if entry.Name() != "plugin.json" {
				continue
			}
			manifestPath = filepath.Join(pluginDir, entry.Name())
		}
		manifest, err := workflowplugin.LoadManifest(manifestPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return index, count, err
		}
		for _, raw := range manifestRawCapabilities(manifest) {
			index[raw.Kind+":"+strings.ToLower(strings.TrimSpace(raw.Value))] = struct{}{}
		}
		count++
	}
	return index, count, nil
}

func providerIndexHas(index map[string]struct{}, kind, value string) bool {
	if len(index) == 0 {
		return false
	}
	_, ok := index[kind+":"+strings.ToLower(strings.TrimSpace(value))]
	return ok
}

type appBuilder struct {
	tax      *Taxonomy
	usage    map[string]*Usage
	findings []Finding
}

func newAppBuilder(tax *Taxonomy) *appBuilder {
	return &appBuilder{
		tax:   tax,
		usage: make(map[string]*Usage),
	}
}

func (b *appBuilder) addRawUsage(kind, value, mode, confidence string, evidence Evidence, hasProvider bool) {
	if taxCap, ok := b.tax.MatchType(kind, value); ok {
		b.addTaxonomyUsage(taxCap, mode, confidence, evidence)
		if !hasProvider && (kind == "module" || kind == "step" || kind == "trigger") {
			b.addFinding(Finding{
				Level:        "warning",
				Code:         "missing-provider",
				CapabilityID: taxCap.ID,
				Message:      fmt.Sprintf("%s type %q is used but no installed plugin declares it", kind, value),
				Evidence:     []Evidence{evidence},
			})
		}
		return
	}
	id := "uncategorized:" + strings.ToLower(kind) + ":" + strings.ToLower(strings.TrimSpace(value))
	b.addUsage(id, "uncategorized", value, mode, confidence, evidence)
	b.addFinding(Finding{
		Level:        "warning",
		Code:         "missing-provider",
		CapabilityID: id,
		Message:      fmt.Sprintf("%s type %q is used but no installed plugin declares it or taxonomy maps it", kind, value),
		Evidence:     []Evidence{evidence},
	})
}

func (b *appBuilder) capabilityIDForRaw(kind, value string) string {
	if taxCap, ok := b.tax.MatchType(kind, value); ok {
		return taxCap.ID
	}
	return "uncategorized:" + strings.ToLower(kind) + ":" + strings.ToLower(strings.TrimSpace(value))
}

func (b *appBuilder) addKnownUsage(id, mode, confidence string, evidence Evidence) {
	taxCap, ok := b.tax.ByID(id)
	if !ok {
		b.addUsage(id, "", id, mode, confidence, evidence)
		return
	}
	b.addTaxonomyUsage(taxCap, mode, confidence, evidence)
}

func (b *appBuilder) addTaxonomyUsage(taxCap *TaxonomyCapability, mode, confidence string, evidence Evidence) {
	b.addUsage(taxCap.ID, taxCap.Category, taxCap.Name, mode, confidence, evidence)
}

func (b *appBuilder) addUsage(id, category, name, mode, confidence string, evidence Evidence) {
	key := id + "|" + mode
	usage, ok := b.usage[key]
	if !ok {
		usage = &Usage{
			CapabilityID: id,
			Category:     category,
			Name:         name,
			Mode:         mode,
			Confidence:   confidence,
		}
		b.usage[key] = usage
	}
	usage.Evidence = append(usage.Evidence, evidence)
}

func (b *appBuilder) addFinding(finding Finding) {
	b.findings = append(b.findings, finding)
}

func (b *appBuilder) profile() *AppProfile {
	usage := make([]Usage, 0, len(b.usage))
	for _, row := range b.usage {
		sort.Slice(row.Evidence, func(i, j int) bool {
			if row.Evidence[i].SourcePath == row.Evidence[j].SourcePath {
				return row.Evidence[i].JSONPath < row.Evidence[j].JSONPath
			}
			return row.Evidence[i].SourcePath < row.Evidence[j].SourcePath
		})
		usage = append(usage, *row)
	}
	sort.Slice(usage, func(i, j int) bool {
		if usage[i].CapabilityID == usage[j].CapabilityID {
			return usage[i].Mode < usage[j].Mode
		}
		return usage[i].CapabilityID < usage[j].CapabilityID
	})
	sort.Slice(b.findings, func(i, j int) bool {
		if b.findings[i].Code == b.findings[j].Code {
			return b.findings[i].CapabilityID < b.findings[j].CapabilityID
		}
		return b.findings[i].Code < b.findings[j].Code
	})
	return &AppProfile{
		Usage:    usage,
		Findings: append([]Finding(nil), b.findings...),
	}
}

func hasTenantEvidence(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, nested := range typed {
			keyLower := strings.ToLower(key)
			if strings.Contains(keyLower, "tenant") {
				return true
			}
			if hasTenantEvidence(nested) {
				return true
			}
		}
	case []any:
		for _, nested := range typed {
			if hasTenantEvidence(nested) {
				return true
			}
		}
	}
	return false
}

func isDataModule(moduleType string) bool {
	lower := strings.ToLower(moduleType)
	return strings.Contains(lower, "storage") || strings.Contains(lower, "database") || strings.Contains(lower, "postgres")
}

func inferredAuthCapabilityID(moduleType string) string {
	lower := strings.ToLower(moduleType)
	switch {
	case strings.Contains(lower, "authz"), strings.Contains(lower, "rbac"), strings.Contains(lower, "permission"):
		return "auth.authz"
	case strings.Contains(lower, "auth"), strings.Contains(lower, "jwt"), strings.Contains(lower, "sso"), strings.Contains(lower, "oidc"), strings.Contains(lower, "passkey"):
		return "auth.authn"
	default:
		return ""
	}
}
