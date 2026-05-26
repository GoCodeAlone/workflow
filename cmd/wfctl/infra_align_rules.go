package main

import (
	"bufio"
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// AlignFinding is a single rule violation emitted by an alignment check.
type AlignFinding struct {
	Rule string // R-A1, R-A2, etc.
	// Severity is one of:
	//   - "FAIL"  — always blocks (exit 1)
	//   - "ERROR" — always blocks (exit 1); used by rules that want to
	//               carry a fix-suggestion in the message (e.g. R-A9)
	//   - "WARN"  — blocks only under --strict
	Severity string
	Resource string // affected resource name
	Message  string // human-readable description
}

// alignContext holds all modules parsed from a config file plus helper indexes.
type alignContext struct {
	modules []config.ModuleConfig

	// indexes built once and reused by rules
	containerServices map[string]config.ModuleConfig // name → infra.container_service
	ciBuilds          []config.ModuleConfig          // all ci.build modules
	databases         []config.ModuleConfig          // all infra.database modules
	secretKeys        map[string]struct{}            // union of secrets.generate/requires keys
	secretGens        []SecretGen                    // secrets.generate entries from top-level secrets block
}

func buildAlignContext(cfgFile string) (*alignContext, error) {
	cfg, err := config.LoadFromFile(cfgFile)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	ctx := &alignContext{
		modules:           cfg.Modules,
		containerServices: map[string]config.ModuleConfig{},
		secretKeys:        map[string]struct{}{},
	}
	if cfg.Secrets != nil {
		ctx.secretGens = cfg.Secrets.Generate
		for _, gen := range cfg.Secrets.Generate {
			ctx.secretKeys[gen.Key] = struct{}{}
		}
		for _, entry := range cfg.Secrets.Entries {
			ctx.secretKeys[entry.Name] = struct{}{}
		}
	}
	for _, m := range cfg.Modules {
		switch {
		case m.Type == "infra.container_service":
			ctx.containerServices[m.Name] = m
		case strings.HasPrefix(m.Type, "ci.build"):
			ctx.ciBuilds = append(ctx.ciBuilds, m)
		case m.Type == "infra.database":
			ctx.databases = append(ctx.databases, m)
		case m.Type == "secrets.generate" || m.Type == "secrets.requires":
			for _, k := range secretModuleKeys(m.Config, "generate") {
				ctx.secretKeys[k] = struct{}{}
			}
			for _, k := range secretModuleKeys(m.Config, "requires") {
				ctx.secretKeys[k] = struct{}{}
			}
		}
	}
	return ctx, nil
}

// ── R-A1: container/runtime alignment ──────────────────────────────────────

func checkRA1(ctx *alignContext) []AlignFinding {
	// Build a set of image names from ci.build containers.
	ciImages := map[string]string{} // image name → dockerfile path
	for _, build := range ctx.ciBuilds {
		if containers, ok := build.Config["containers"]; ok {
			if items, ok := containers.([]any); ok {
				for _, item := range items {
					if m, ok := item.(map[string]any); ok {
						name, _ := m["name"].(string)
						df, _ := m["dockerfile"].(string)
						if name != "" {
							ciImages[name] = df
						}
					}
				}
			}
		}
	}

	var findings []AlignFinding
	for _, svc := range ctx.containerServices {
		image, _ := svc.Config["image"].(string)
		if image == "" {
			continue
		}
		// Parse image name (strip tag)
		imageName := image
		if idx := strings.LastIndex(image, ":"); idx > 0 {
			// Handle registry/name:tag — only strip the tag portion
			imageName = image[:idx]
		}
		// Get just the final name component for matching
		parts := strings.Split(imageName, "/")
		shortName := parts[len(parts)-1]

		// R-A1a: orphaned image reference — only enforce when at least one
		// ci.build module exists. Projects with no build phase use pre-built
		// external images (redis:7, postgres:15, etc.) and should not be flagged.
		if len(ctx.ciBuilds) > 0 {
			if _, found := ciImages[shortName]; !found {
				findings = append(findings, AlignFinding{
					Rule:     "R-A1",
					Severity: "FAIL",
					Resource: svc.Name,
					Message:  fmt.Sprintf("orphaned image reference %q: no ci.build container named %q", image, shortName),
				})
				// Skip Dockerfile checks if we can't match the build
				continue
			}
		}

		// Resolve dockerfile path
		dfPath := resolveDockerfilePath(svc, ciImages, shortName)
		if dfPath == "" {
			continue
		}
		if _, err := os.Stat(dfPath); err != nil {
			// Dockerfile not on disk — skip lint
			continue
		}

		// R-A1b: USER directive check
		if f := checkDockerfileUser(svc.Name, dfPath); f != nil {
			findings = append(findings, *f)
		}

		// R-A1c: EXPOSE vs http_port/internal_ports check
		if f := checkDockerfileExpose(svc, dfPath); f != nil {
			findings = append(findings, *f...)
		}
	}
	return findings
}

func resolveDockerfilePath(svc config.ModuleConfig, ciImages map[string]string, shortName string) string {
	// Check service config directly
	if df, ok := svc.Config["dockerfile"].(string); ok && df != "" {
		return df
	}
	// Fall back to ci.build container match
	if df, ok := ciImages[shortName]; ok && df != "" {
		return df
	}
	return ""
}

func checkDockerfileUser(svcName, dfPath string) *AlignFinding {
	f, err := os.Open(dfPath)
	if err != nil {
		return nil
	}
	defer f.Close()

	var foundUser string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(strings.ToUpper(line), "USER ") {
			foundUser = strings.TrimSpace(line[5:])
		}
	}

	if foundUser == "" {
		return &AlignFinding{
			Rule:     "R-A1",
			Severity: "FAIL",
			Resource: svcName,
			Message:  fmt.Sprintf("Dockerfile %q has no USER directive (container runs as root)", dfPath),
		}
	}
	// FAIL if explicitly root
	if foundUser == "root" || foundUser == "0" {
		return &AlignFinding{
			Rule:     "R-A1",
			Severity: "FAIL",
			Resource: svcName,
			Message:  fmt.Sprintf("Dockerfile %q sets USER root — containers must not run as root", dfPath),
		}
	}
	return nil
}

func checkDockerfileExpose(svc config.ModuleConfig, dfPath string) *[]AlignFinding {
	f, err := os.Open(dfPath)
	if err != nil {
		return nil
	}
	defer f.Close()

	var exposedPorts []int
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		upper := strings.ToUpper(line)
		if strings.HasPrefix(upper, "EXPOSE ") {
			parts := strings.Fields(line[7:])
			for _, p := range parts {
				// Strip /tcp or /udp
				portStr := strings.Split(p, "/")[0]
				if port, err := strconv.Atoi(portStr); err == nil {
					exposedPorts = append(exposedPorts, port)
				}
			}
		}
	}

	if len(exposedPorts) == 0 {
		return nil
	}

	// Gather declared ports from config
	declaredPorts := map[int]struct{}{}
	if httpPort, ok := svc.Config["http_port"]; ok {
		if p := toInt(httpPort); p > 0 {
			declaredPorts[p] = struct{}{}
		}
	}
	if raw, ok := svc.Config["internal_ports"]; ok {
		if items, ok := raw.([]any); ok {
			for _, item := range items {
				if p := toInt(item); p > 0 {
					declaredPorts[p] = struct{}{}
				}
			}
		}
	}

	if len(declaredPorts) == 0 {
		return nil
	}

	var findings []AlignFinding
	for _, ep := range exposedPorts {
		if _, ok := declaredPorts[ep]; !ok {
			findings = append(findings, AlignFinding{
				Rule:     "R-A1",
				Severity: "FAIL",
				Resource: svc.Name,
				Message:  fmt.Sprintf("Dockerfile EXPOSEs port %d but it is not in http_port or internal_ports", ep),
			})
		}
	}
	if len(findings) == 0 {
		return nil
	}
	return &findings
}

// ── R-A2: health-check alignment ───────────────────────────────────────────

func checkRA2(ctx *alignContext, strictHealth bool) []AlignFinding {
	var findings []AlignFinding
	for _, svc := range ctx.containerServices {
		path := healthCheckPath(svc.Config)
		if path == "" {
			continue
		}
		srcDir, _ := svc.Config["src_dir"].(string)
		if srcDir == "" {
			srcDir = "."
		}

		found, err := pathExistsInSource(srcDir, path)
		if err != nil || found {
			continue
		}

		severity := "WARN"
		if strictHealth {
			severity = "FAIL"
		}
		findings = append(findings, AlignFinding{
			Rule:     "R-A2",
			Severity: severity,
			Resource: svc.Name,
			Message:  fmt.Sprintf("health check path %q not found in source tree %q", path, srcDir),
		})
	}
	return findings
}

func healthCheckPath(cfg map[string]any) string {
	// Try health_check.path (snake_case)
	if hc, ok := cfg["health_check"]; ok {
		if m, ok := hc.(map[string]any); ok {
			if p, ok := m["path"].(string); ok {
				return p
			}
		}
	}
	// Try healthCheck.path (camelCase)
	if hc, ok := cfg["healthCheck"]; ok {
		if m, ok := hc.(map[string]any); ok {
			if p, ok := m["path"].(string); ok {
				return p
			}
		}
	}
	return ""
}

var sourceExtensions = map[string]struct{}{
	".go": {}, ".js": {}, ".ts": {}, ".py": {}, ".rs": {},
}

func pathExistsInSource(srcDir, path string) (bool, error) {
	var found bool
	err := filepath.Walk(srcDir, func(fp string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || found {
			return nil
		}
		if _, ok := sourceExtensions[filepath.Ext(fp)]; !ok {
			return nil
		}
		data, readErr := os.ReadFile(fp)
		if readErr != nil {
			return readErr
		}
		if strings.Contains(string(data), path) {
			found = true
		}
		return nil
	})
	return found, err
}

// ── R-A3: service-to-service DNS alignment ──────────────────────────────────

var internalDNSRe = regexp.MustCompile(`([a-z][a-z0-9-]+)\.internal:(\d+)`)

func checkRA3(ctx *alignContext) []AlignFinding {
	var findings []AlignFinding
	for _, m := range ctx.modules {
		envVars := extractEnvVars(m.Config)
		for _, val := range envVars {
			matches := internalDNSRe.FindAllStringSubmatch(val, -1)
			for _, match := range matches {
				hostname := match[1]
				portStr := match[2]
				port, _ := strconv.Atoi(portStr)

				target, ok := ctx.containerServices[hostname]
				if !ok {
					findings = append(findings, AlignFinding{
						Rule:     "R-A3",
						Severity: "FAIL",
						Resource: m.Name,
						Message:  fmt.Sprintf("DNS reference to %q.internal not resolvable: no container_service named %q", hostname, hostname),
					})
					continue
				}

				// Verify port matches http_port or internal_ports
				if !serviceHasPort(target, port) {
					findings = append(findings, AlignFinding{
						Rule:     "R-A3",
						Severity: "FAIL",
						Resource: m.Name,
						Message:  fmt.Sprintf("DNS reference to %s.internal:%d: port %d not declared in service %q (http_port or internal_ports)", hostname, port, port, hostname),
					})
				}
			}
		}
	}
	return findings
}

func extractEnvVars(cfg map[string]any) map[string]string {
	result := map[string]string{}
	if raw, ok := cfg["env_vars"]; ok {
		if m, ok := raw.(map[string]any); ok {
			for k, v := range m {
				if s, ok := v.(string); ok {
					result[k] = s
				}
			}
		}
	}
	return result
}

func serviceHasPort(svc config.ModuleConfig, port int) bool {
	if httpPort := toInt(svc.Config["http_port"]); httpPort == port {
		return true
	}
	if raw, ok := svc.Config["internal_ports"]; ok {
		if items, ok := raw.([]any); ok {
			for _, item := range items {
				if toInt(item) == port {
					return true
				}
			}
		}
	}
	return false
}

// ── R-A4: env-var resolution ───────────────────────────────────────────────

var envTokenRe = regexp.MustCompile(`\$\{([^}]+)\}`)

func checkRA4(ctx *alignContext) []AlignFinding {
	var findings []AlignFinding
	for _, m := range ctx.modules {
		envVars := extractEnvVars(m.Config)
		for _, val := range envVars {
			tokens := envTokenRe.FindAllStringSubmatch(val, -1)
			for _, tok := range tokens {
				varName := tok[1]
				if _, resolved := ctx.secretKeys[varName]; resolved {
					continue
				}
				if os.Getenv(varName) != "" {
					continue
				}
				findings = append(findings, AlignFinding{
					Rule:     "R-A4",
					Severity: "FAIL",
					Resource: m.Name,
					Message:  fmt.Sprintf("unresolved env var: ${%s}", varName),
				})
			}
		}
	}
	return findings
}

// ── R-A5: migrations alignment ─────────────────────────────────────────────

func checkRA5(ctx *alignContext) []AlignFinding {
	// Build a set of ci.build container image names
	ciBuildImages := map[string]struct{}{}
	for _, build := range ctx.ciBuilds {
		if containers, ok := build.Config["containers"]; ok {
			if items, ok := containers.([]any); ok {
				for _, item := range items {
					if m, ok := item.(map[string]any); ok {
						if name, ok := m["name"].(string); ok && name != "" {
							ciBuildImages[name] = struct{}{}
						}
					}
				}
			}
		}
	}

	var findings []AlignFinding
	for svcName, svc := range ctx.containerServices {
		preDeploy, ok := svc.Config["pre_deploy"]
		if !ok {
			continue
		}
		pdMap, ok := preDeploy.(map[string]any)
		if !ok {
			continue
		}
		kind, _ := pdMap["kind"].(string)
		if kind != "migrate" {
			continue
		}

		// R-A5a: must have an infra.database with trusted_sources entry for this service
		dbFound := false
		for _, db := range ctx.databases {
			if dbTrustsService(db, svcName) {
				dbFound = true
				break
			}
		}
		if !dbFound {
			// Also accept any DB at all (spec says "the same env's infra.database must declare trusted_sources")
			if len(ctx.databases) == 0 {
				findings = append(findings, AlignFinding{
					Rule:     "R-A5",
					Severity: "FAIL",
					Resource: svcName,
					Message:  fmt.Sprintf("service %q has pre_deploy migrate but no infra.database module exists", svcName),
				})
			} else {
				findings = append(findings, AlignFinding{
					Rule:     "R-A5",
					Severity: "FAIL",
					Resource: svcName,
					Message:  fmt.Sprintf("service %q has pre_deploy migrate but no infra.database declares trusted_sources for it", svcName),
				})
			}
		}

		// R-A5b: pre_deploy image must reference a ci.build container
		if image, ok := pdMap["image"].(string); ok && image != "" {
			imageName := image
			if idx := strings.LastIndex(image, ":"); idx > 0 {
				imageName = image[:idx]
			}
			parts := strings.Split(imageName, "/")
			shortName := parts[len(parts)-1]
			if len(ciBuildImages) > 0 {
				if _, ok := ciBuildImages[shortName]; !ok {
					findings = append(findings, AlignFinding{
						Rule:     "R-A5",
						Severity: "FAIL",
						Resource: svcName,
						Message:  fmt.Sprintf("pre_deploy image %q not found in any ci.build container", image),
					})
				}
			}
		}
	}
	return findings
}

func dbTrustsService(db config.ModuleConfig, svcName string) bool {
	raw, ok := db.Config["trusted_sources"]
	if !ok {
		return false
	}
	items, ok := raw.([]any)
	if !ok {
		return false
	}
	for _, item := range items {
		if m, ok := item.(map[string]any); ok {
			if m["type"] == "app" && m["value"] == svcName {
				return true
			}
		}
	}
	return false
}

// ── R-A6: network/exposure alignment ───────────────────────────────────────

var internalServiceNameRe = regexp.MustCompile(`(^|-)(nats|redis|db|broker|internal)($|-)`)

func checkRA6(ctx *alignContext) []AlignFinding {
	var findings []AlignFinding
	for _, svc := range ctx.containerServices {
		expose, _ := svc.Config["expose"].(string)
		httpPort := toInt(svc.Config["http_port"])

		// R-A6a: expose:internal AND http_port is mutually exclusive
		if expose == "internal" && httpPort > 0 {
			findings = append(findings, AlignFinding{
				Rule:     "R-A6",
				Severity: "FAIL",
				Resource: svc.Name,
				Message:  "expose: internal with http_port is mutually exclusive",
			})
		}

		// R-A6b: name pattern suggests internal service but expose:internal not set
		if internalServiceNameRe.MatchString(svc.Name) && httpPort > 0 && expose != "internal" {
			findings = append(findings, AlignFinding{
				Rule:     "R-A6",
				Severity: "WARN",
				Resource: svc.Name,
				Message:  "internal service should use expose: internal",
			})
		}
	}
	return findings
}

// ── R-A7: plan-output sanity ───────────────────────────────────────────────

func checkRA7(plan *interfaces.IaCPlan, maxChanges int) []AlignFinding {
	if plan == nil {
		return nil
	}
	var findings []AlignFinding

	for i := range plan.Actions {
		action := &plan.Actions[i]
		if action.Action == "delete" {
			if protected, _ := action.Resource.Config["protected"].(bool); protected {
				findings = append(findings, AlignFinding{
					Rule:     "R-A7",
					Severity: "FAIL",
					Resource: action.Resource.Name,
					Message:  fmt.Sprintf("plan deletes protected resource %q", action.Resource.Name),
				})
			}
		}
	}

	if len(plan.Actions) > maxChanges {
		findings = append(findings, AlignFinding{
			Rule:     "R-A7",
			Severity: "WARN",
			Resource: "(plan)",
			Message:  fmt.Sprintf("plan has %d actions, exceeding max-changes limit of %d", len(plan.Actions), maxChanges),
		})
	}

	return findings
}

// ── R-A8: WebAuthn alignment ───────────────────────────────────────────────

func checkRA8(ctx *alignContext) []AlignFinding {
	var findings []AlignFinding
	for _, m := range ctx.modules {
		envVars := extractEnvVars(m.Config)
		rpID, hasRPID := envVars["WEBAUTHN_RP_ID"]
		origin, hasOrigin := envVars["WEBAUTHN_ORIGIN"]
		if !hasRPID || !hasOrigin {
			continue
		}

		// Skip if either value is an unresolved ${...} reference — url.Parse
		// accepts "${VAR}" without error but returns an empty hostname, causing
		// a spurious FAIL when the value is runtime-injected via secrets.
		if strings.Contains(origin, "${") || strings.Contains(rpID, "${") {
			continue
		}

		u, err := url.Parse(origin)
		if err != nil {
			findings = append(findings, AlignFinding{
				Rule:     "R-A8",
				Severity: "FAIL",
				Resource: m.Name,
				Message:  fmt.Sprintf("WEBAUTHN_ORIGIN %q is not a valid URL: %v", origin, err),
			})
			continue
		}

		host := u.Hostname()
		if host != rpID {
			findings = append(findings, AlignFinding{
				Rule:     "R-A8",
				Severity: "FAIL",
				Resource: m.Name,
				Message:  fmt.Sprintf("WEBAUTHN_RP_ID %q does not match WEBAUTHN_ORIGIN host %q", rpID, host),
			})
		}
	}
	return findings
}

// ── R-A9: suspicious provider_credential key suffix ────────────────────────

// checkRA9 fires (as an ERROR) when a secrets.generate entry with type
// "provider_credential" uses a key that already ends with a known sub-key
// suffix (e.g. "_access_key", "_secret_key"). This is the doubled-create
// anti-pattern: each sub-keyed entry causes bootstrapSecrets to create a
// separate cloud credential, producing an orphaned pair of keys instead of
// a single canonical credential. The canonical form is to use the root key
// (e.g. "SPACES") and let bootstrapSecrets auto-derive the sub-key names
// from providerCredentialSubKeys[source].
//
// The rule only fires for sources registered in providerCredentialSubKeys.
// Unknown sources are skipped — we cannot predict their sub-key names.
//
// Severity: ERROR (was WARN through rev2 of the spaces-key plan; flipped to
// ERROR in rev3 so `wfctl infra align --strict` blocks deploy when the
// anti-pattern is present).
func checkRA9(ctx *alignContext) []AlignFinding {
	var findings []AlignFinding
	for _, gen := range ctx.secretGens {
		if gen.Type != "provider_credential" {
			continue
		}
		subs, known := subKeysForSource(gen.Source)
		if !known {
			continue
		}
		for _, sub := range subs {
			suffix := "_" + sub
			if strings.HasSuffix(gen.Key, suffix) {
				findings = append(findings, AlignFinding{
					Rule:     "R-A9",
					Severity: "ERROR",
					Resource: gen.Key,
					Message: fmt.Sprintf(
						"provider_credential key %q ends in %q; use canonical key %q — bootstrap auto-derives the sub-keys for source %q",
						gen.Key, suffix, strings.TrimSuffix(gen.Key, suffix), gen.Source,
					),
				})
				break // one finding per gen entry is enough
			}
		}
	}
	return findings
}

// ── R-A10: provider.ValidatePlan dispatch ──────────────────────────────────

// ra10LogInfo is the sink used to surface PlanDiagnosticInfo diagnostics
// without emitting an AlignFinding. The default writes to os.Stderr; tests
// override this var to capture the log line and assert on it.
var ra10LogInfo = func(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format, args...)
}

// checkRA10_provider_validate_plan iterates the loaded providers, type-asserts
// each as interfaces.ProviderValidator, calls ValidatePlan(plan) and renders
// each returned PlanDiagnostic according to its severity tier.
//
// Three-tier severity mapping (matches the rev10 plan T4.2 acceptance
// criteria: "Errors → align failures; Warnings → warnings; Info → logs"):
//
//	PlanDiagnosticError   → AlignFinding{Severity: "FAIL"} (always non-zero exit)
//	PlanDiagnosticWarning → AlignFinding{Severity: "WARN"} (non-zero only under --strict)
//	PlanDiagnosticInfo    → logged to stderr via ra10LogInfo; NO AlignFinding,
//	                        never affects exit code (Info exists precisely so a
//	                        provider can surface a hint without breaking
//	                        `--strict` CI gates).
//
// Resource label: prefer the diagnostic's Resource field; fall back to
// "<provider-name>:plan" for plan-level findings so the rendered table (and
// the stderr log line for Info) always identifies the source provider.
//
// No-op when plan is nil or providers is empty (matches R-A7's predicate so
// running `wfctl infra align` without --plan never triggers R-A10).
//
// Naming follows the plan T4.2 spec literally; existing rule helpers use the
// shorter checkRA<N> form, but the descriptive suffix here documents the
// rule's intent at the call site in infra_align.go.
// checkRA10_provider_validate_plan dispatches the R-A10 ValidatePlan rule
// across all loaded providers. Per code-review IMPORTANT-2 (PR 618 round 4):
// takes ctx so the typed-RPC ValidatePlan call honors caller cancellation /
// deadline rather than dropping it via context.Background().
func checkRA10_provider_validate_plan(ctx context.Context, providers []interfaces.IaCProvider, plan *interfaces.IaCPlan) []AlignFinding {
	if plan == nil || len(providers) == 0 {
		return nil
	}
	var findings []AlignFinding
	for _, p := range providers {
		// Per Task 17 (ADR-0028): pure typed-pb dispatch — no
		// interfaces.X fallback. Non-typed providers are silently
		// skipped (R-A10's "treat unimplemented as not-applicable"
		// behavior is preserved at the typed-adapter accessor level).
		adapter, ok := p.(*typedIaCAdapter)
		if !ok {
			continue
		}
		cli := adapter.Validator()
		if cli == nil {
			continue
		}
		diags := validatePlanTyped(ctx, cli, plan)
		for _, d := range diags {
			// resource: rendered table label (provider-qualified for plan-
			// level findings so the table always identifies the source).
			// logResource: log-line identifier; for plan-level findings use
			// the bare "plan" sentinel so the documented
			// `R-A10 [info] <provider>/<resource>: ...` format renders as
			// `<provider>/plan: ...` instead of the redundant
			// `<provider>/<provider>:plan: ...`.
			resource := d.Resource
			logResource := d.Resource
			if resource == "" {
				resource = fmt.Sprintf("%s:plan", p.Name())
				logResource = "plan"
			}
			msg := d.Message
			if d.Field != "" {
				msg = fmt.Sprintf("%s (field: %s)", d.Message, d.Field)
			}
			switch d.Severity {
			case interfaces.PlanDiagnosticError:
				findings = append(findings, AlignFinding{
					Rule:     "R-A10",
					Severity: "FAIL",
					Resource: resource,
					Message:  msg,
				})
			case interfaces.PlanDiagnosticWarning:
				findings = append(findings, AlignFinding{
					Rule:     "R-A10",
					Severity: "WARN",
					Resource: resource,
					Message:  msg,
				})
			case interfaces.PlanDiagnosticInfo:
				// Info tier: log to stderr; no AlignFinding so exit code
				// is never affected (even under --strict).
				ra10LogInfo("R-A10 [info] %s/%s: %s\n", p.Name(), logResource, msg)
			default:
				// Unknown severity — treat conservatively as WARN so it
				// doesn't slip past --strict.
				findings = append(findings, AlignFinding{
					Rule:     "R-A10",
					Severity: "WARN",
					Resource: resource,
					Message:  msg,
				})
			}
		}
	}
	return findings
}

// ── utilities ──────────────────────────────────────────────────────────────

// toInt converts an any value to int, handling both int and float64
// (YAML unmarshal produces float64 for numbers).
func toInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case string:
		i, _ := strconv.Atoi(n)
		return i
	}
	return 0
}
