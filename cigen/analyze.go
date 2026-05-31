package cigen

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/GoCodeAlone/workflow/config"
)

// varRefPattern matches ${VAR_NAME} references in config strings.
var varRefPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// Options controls the behaviour of Analyze.
type Options struct {
	// WfctlVersion is the version string to embed in the plan (e.g. "v0.66.0" or "latest").
	WfctlVersion string
	// DefaultBranch overrides the default branch (defaults to "main").
	DefaultBranch string
	// Runner overrides the GitHub Actions runner label (defaults to "ubuntu-latest").
	Runner string
	// PhaseConfig is an optional second config path that becomes a prereq DeployPhase
	// inserted before the main phase.
	PhaseConfig string
	// Project overrides the project name derived from the config file.
	Project string
	// ConfigPathAlias, when set, is used verbatim as the primary phase's
	// DeployPhase.ConfigPath instead of the real (possibly temporary or
	// absolute) filesystem path Analyze was handed. Callers that read the
	// config from a non-repo location — e.g. the MCP server, which writes
	// yaml_content to an os.CreateTemp file — MUST set this to a stable,
	// repo-relative logical name (e.g. "deploy.yaml" or the project name)
	// so generated CI steps and path filters reference a real checkout path
	// rather than a deleted /tmp file.
	ConfigPathAlias string
	// PhaseConfigAlias is the ConfigPathAlias equivalent for the prereq phase.
	// When set, it is used verbatim as the prereq DeployPhase.ConfigPath
	// instead of the relativized PhaseConfig filesystem path.
	PhaseConfigAlias string
}

// Analyze reads the workflow config files in configs and derives a CIPlan.
// configs must be non-empty; the first entry is the primary config.
// opts.PhaseConfig, if set, is loaded as a prerequisite phase.
func Analyze(configs []string, opts Options) (*CIPlan, error) {
	if len(configs) == 0 {
		return nil, fmt.Errorf("cigen.Analyze: at least one config path is required")
	}

	primaryPath := configs[0]
	cfg, err := config.LoadFromFile(primaryPath)
	if err != nil {
		return nil, fmt.Errorf("cigen.Analyze: load %s: %w", primaryPath, err)
	}

	plan := &CIPlan{
		WfctlVersion:  resolveVersion(opts.WfctlVersion),
		DefaultBranch: resolveDefault(opts.DefaultBranch, "main"),
		Runner:        resolveDefault(opts.Runner, "ubuntu-latest"),
		Triggers: TriggerSpec{
			PR:       true,
			PushMain: true,
			Dispatch: true,
		},
		Warnings: []string{},
	}

	// Project name
	plan.Project = resolveProject(opts.Project, primaryPath)

	// PluginInstall: any plugin/* or infra.* module type, or .wfctl-lock.yaml sibling
	plan.PluginInstall = detectPluginInstall(cfg, primaryPath)

	// PlanGuard: any ModuleConfig.Protected == true
	plan.PlanGuard = detectPlanGuard(cfg)

	// Migrations
	plan.Migrations = deriveMigrations(cfg)

	// Build: Dockerfile sibling
	plan.Build = deriveBuild(primaryPath)

	// Secrets
	plan.Secrets = deriveSecrets(cfg, plan.Migrations)

	// Smoke
	plan.Smoke = deriveSmoke(cfg)

	// Warnings
	plan.Warnings = deriveWarnings(cfg, plan.Migrations, plan.Secrets)

	// Phases. The primary phase's config path is the stable logical alias when
	// provided (MCP path), otherwise the real path relativized to the working
	// directory so the generated `paths:` trigger filter and `--config` step
	// args reference a checkout-relative path rather than an absolute or /tmp
	// path that never matches a CI checkout.
	primaryConfigPath := opts.ConfigPathAlias
	if primaryConfigPath == "" {
		primaryConfigPath = relativizeConfigPath(primaryPath)
	}

	// Per-phase secret scoping (multi-phase only). The prereq phase is scoped to
	// the secrets ITS config references; the deploy (last) phase keeps the
	// primary union (which already includes the migration DBEnv via deriveSecrets).
	var prereqSecrets []SecretRef
	scoped := false
	phaseConfigPath := opts.PhaseConfig
	if phaseConfigPath != "" {
		if opts.PhaseConfigAlias != "" {
			phaseConfigPath = opts.PhaseConfigAlias
		} else {
			phaseConfigPath = relativizeConfigPath(opts.PhaseConfig)
		}
		if pcfg, perr := config.LoadFromFile(opts.PhaseConfig); perr == nil {
			// deriveMigrations is primary-only: the migrating phase is the LAST
			// phase, so the prereq never gets a migration DBEnv (pass nil).
			prereqSecrets = deriveSecrets(pcfg, nil)
			scoped = true
		} else {
			plan.Warnings = append(plan.Warnings,
				fmt.Sprintf("per-phase secret scoping unavailable: phase config %q not loadable (%v); using union", opts.PhaseConfig, perr))
		}
	}

	plan.Phases = derivePhases(primaryConfigPath, phaseConfigPath, plan.Secrets, prereqSecrets, scoped)

	return plan, nil
}

// relativizeConfigPath converts a config path to one relative to the current
// working directory. If the path cannot be made relative or escapes cwd
// (resolves to a parent via ".."), it falls back to the base filename so the
// generated CI never references an absolute or out-of-tree path.
func relativizeConfigPath(path string) string {
	if path == "" {
		return path
	}
	cwd, err := os.Getwd()
	if err != nil {
		return filepath.Base(path)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Base(path)
	}
	rel, err := filepath.Rel(cwd, abs)
	if err != nil {
		return filepath.Base(path)
	}
	// Escapes cwd (e.g. "../sibling/deploy.yaml") — a CI checkout would not
	// contain it at that path, so fall back to the base name.
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return filepath.Base(path)
	}
	return rel
}

// resolveVersion returns v if non-empty, otherwise "latest".
func resolveVersion(v string) string {
	if v != "" {
		return v
	}
	return "latest"
}

// resolveDefault returns val if non-empty, otherwise def.
func resolveDefault(val, def string) string {
	if val != "" {
		return val
	}
	return def
}

// resolveProject derives a project name from opts.Project or the config file path.
func resolveProject(explicit, configPath string) string {
	if explicit != "" {
		return explicit
	}
	base := filepath.Base(configPath)
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)
	// Use the directory name if the file is "app" or "deploy" or similar generic names
	if name == "app" || name == "deploy" || name == "infra" {
		dir := filepath.Dir(configPath)
		if dir != "." && dir != "" {
			return filepath.Base(dir)
		}
	}
	return name
}

// detectPluginInstall returns true if the config references any plugin or infra module,
// or if a .wfctl-lock.yaml file exists in the config's directory.
func detectPluginInstall(cfg *config.WorkflowConfig, configPath string) bool {
	for _, m := range cfg.Modules {
		if strings.HasPrefix(m.Type, "infra.") ||
			strings.HasPrefix(m.Type, "iac.") ||
			strings.HasPrefix(m.Type, "plugin.") ||
			strings.HasPrefix(m.Type, "analytics.") {
			return true
		}
	}
	// Check for .wfctl-lock.yaml sibling
	dir := filepath.Dir(configPath)
	lockPath := filepath.Join(dir, ".wfctl-lock.yaml")
	if _, err := os.Stat(lockPath); err == nil {
		return true
	}
	// Check if config file has a requires.plugins section
	if cfg.Requires != nil && len(cfg.Requires.Plugins) > 0 {
		return true
	}
	return false
}

// detectPlanGuard returns true if any module has Protected == true.
func detectPlanGuard(cfg *config.WorkflowConfig) bool {
	for _, m := range cfg.Modules {
		if m.Protected {
			return true
		}
	}
	return false
}

// deriveMigrations extracts migration config from the first ci.migrations entry.
func deriveMigrations(cfg *config.WorkflowConfig) *MigrationsSpec {
	if cfg.CI == nil || len(cfg.CI.Migrations) == 0 {
		return nil
	}
	m := cfg.CI.Migrations[0]
	spec := &MigrationsSpec{
		DBEnv:  m.Database.Env,
		Source: m.SourceDir,
	}
	if spec.DBEnv == "" {
		return nil
	}
	// Derive --env only when exactly one environment is declared (unambiguous).
	if len(m.Environments) == 1 {
		for envName := range m.Environments {
			spec.Env = envName
		}
	}
	return spec
}

// deriveBuild checks for a Dockerfile in the config directory.
func deriveBuild(configPath string) *BuildSpec {
	dir := filepath.Dir(configPath)
	dockerfilePath := filepath.Join(dir, "Dockerfile")
	if _, err := os.Stat(dockerfilePath); err == nil {
		return &BuildSpec{Docker: true}
	}
	return nil
}

// deriveSecrets builds the union of all secret references.
func deriveSecrets(cfg *config.WorkflowConfig, migrations *MigrationsSpec) []SecretRef {
	seen := make(map[string]bool)
	var ordered []string

	addSecret := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		ordered = append(ordered, name)
	}

	// 1. secrets.entries
	if cfg.Secrets != nil {
		for _, entry := range cfg.Secrets.Entries {
			addSecret(entry.Name)
		}
	}

	// 2. ${VAR} refs from module Config["env_vars_secret"] values
	for _, m := range cfg.Modules {
		if m.Config == nil {
			continue
		}
		if evs, ok := m.Config["env_vars_secret"]; ok {
			extractVarRefs(evs, addSecret)
		}
		// 3. iac.provider token/spaces keys
		if strings.HasPrefix(m.Type, "iac.provider") {
			for _, key := range []string{"token", "spaces_access_key", "spaces_secret_key", "accessKey", "secretKey"} {
				if val, ok := m.Config[key]; ok {
					if s, ok := val.(string); ok {
						extractVarRefsFromString(s, addSecret)
					}
				}
			}
		}
	}

	// 4. migrations DBEnv
	if migrations != nil && migrations.DBEnv != "" {
		addSecret(migrations.DBEnv)
	}

	sort.Strings(ordered)
	refs := make([]SecretRef, 0, len(ordered))
	for _, name := range ordered {
		refs = append(refs, SecretRef{Name: name})
	}
	return refs
}

// extractVarRefs navigates an interface{} that may be a map[string]any or
// string and calls add for each ${VAR} reference found.
func extractVarRefs(v any, add func(string)) {
	switch val := v.(type) {
	case string:
		extractVarRefsFromString(val, add)
	case map[string]any:
		for _, mv := range val {
			extractVarRefs(mv, add)
		}
	case map[any]any:
		for _, mv := range val {
			extractVarRefs(mv, add)
		}
	}
}

// extractVarRefsFromString extracts ${VAR} references from a string.
func extractVarRefsFromString(s string, add func(string)) {
	for _, match := range varRefPattern.FindAllStringSubmatch(s, -1) {
		if len(match) == 2 {
			add(match[1])
		}
	}
}

// deriveSmoke extracts a smoke test spec from an infra.container_service module.
func deriveSmoke(cfg *config.WorkflowConfig) *SmokeSpec {
	for _, m := range cfg.Modules {
		if m.Type != "infra.container_service" {
			continue
		}
		if m.Config == nil {
			continue
		}
		// Get health_check http_path
		path := extractHealthCheckPath(m.Config)
		if path == "" {
			path = "/healthz"
		}
		// Get primary domain
		domain := extractPrimaryDomain(m.Config)
		if domain == "" {
			continue
		}
		return &SmokeSpec{
			URL:  "https://" + domain + path,
			Path: path,
		}
	}
	return nil
}

// extractHealthCheckPath extracts the http_path from a module's health_check config.
func extractHealthCheckPath(cfg map[string]any) string {
	hc, ok := cfg["health_check"]
	if !ok {
		return ""
	}
	switch v := hc.(type) {
	case map[string]any:
		if path, ok := v["http_path"].(string); ok {
			return path
		}
	case map[any]any:
		if path, ok := v["http_path"].(string); ok {
			return path
		}
	}
	return ""
}

// extractPrimaryDomain extracts the primary domain from a module's domains config.
func extractPrimaryDomain(cfg map[string]any) string {
	domains, ok := cfg["domains"]
	if !ok {
		return ""
	}
	if v, ok := domains.([]any); ok {
		for _, d := range v {
			switch dm := d.(type) {
			case map[string]any:
				if dt, ok := dm["type"].(string); ok && strings.EqualFold(dt, "PRIMARY") {
					if domain, ok := dm["domain"].(string); ok {
						return domain
					}
				}
			case map[any]any:
				if dt, ok := dm["type"].(string); ok && strings.EqualFold(dt, "PRIMARY") {
					if domain, ok := dm["domain"].(string); ok {
						return domain
					}
				}
			}
		}
	}
	return ""
}

// derivePhases builds the ordered list of deploy phases.
func derivePhases(primaryPath, phaseConfig string, primarySecrets, prereqSecrets []SecretRef, scoped bool) []DeployPhase {
	var phases []DeployPhase
	if phaseConfig != "" {
		phases = append(phases, DeployPhase{
			Name:       "prereq",
			ConfigPath: phaseConfig,
			Secrets:    prereqSecrets,
			Scoped:     scoped,
		})
	}
	deploy := DeployPhase{
		Name:       "deploy",
		ConfigPath: primaryPath,
	}
	// The deploy phase is scoped to the primary union only in the multi-phase
	// case (scoped==true means the prereq loaded). Single-phase stays unscoped:
	// the union IS its scope, so the renderer's union fallback applies unchanged.
	if scoped {
		deploy.Secrets = primarySecrets
		deploy.Scoped = true
	}
	phases = append(phases, deploy)
	return phases
}

// upperCasePattern matches valid GitHub Actions secret name pattern.
var upperCasePattern = regexp.MustCompile(`^[A-Z0-9_]+$`)

// deriveWarnings produces advisory warnings for the operator.
func deriveWarnings(cfg *config.WorkflowConfig, migrations *MigrationsSpec, secrets []SecretRef) []string {
	var warnings []string

	// (a) state-derived warning: migrations DBEnv may be hash-suffixed in the real GitHub secret
	if migrations != nil && migrations.DBEnv != "" {
		warnings = append(warnings,
			fmt.Sprintf("secret %q is populated by IaC output — the real GitHub secret name may differ (e.g. include a resource hash suffix); verify the secret name matches what wfctl infra bootstrap writes",
				migrations.DBEnv))
	}

	// (b) case/alias warning: secret names not matching ^[A-Z0-9_]+$
	for _, s := range secrets {
		if !upperCasePattern.MatchString(s.Name) {
			warnings = append(warnings,
				fmt.Sprintf("secret %q does not match ^[A-Z0-9_]+$ — the config casing is preserved as-is; you may need a GitHub-side alias if the platform normalises secret names to upper-case",
					s.Name))
		}
	}

	// (c) migrations environment ambiguity: ≥2 declared → --env omitted.
	if cfg.CI != nil && len(cfg.CI.Migrations) > 0 {
		if n := len(cfg.CI.Migrations[0].Environments); n >= 2 {
			warnings = append(warnings,
				fmt.Sprintf("migrations environment ambiguous (%d declared); --env omitted — set it in the generated workflow", n))
		}
	}

	return warnings
}
