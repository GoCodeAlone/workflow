// Package legacydo holds the read-only data and message formatters for the
// legacy DigitalOcean module + step types removed in issue #617. Lives in
// internal/ so that both module/ and modernize/ can import it without a
// cycle (module transitively imports modernize via plugin, so modernize
// cannot import module).
package legacydo

import (
	"fmt"
	"sort"
	"strings"
)

// RemovedInVersion is the workflow tag that ships issue #617's force-cutover.
// Used in every legacy-DO migration error and in the wfctl modernize rule.
// Update both this constant and the docs/migrations/v<X>-godo-removal.md
// filename when the release tag is finalised.
const RemovedInVersion = "v0.52.0"

// ModuleTypes maps each removed legacy DigitalOcean module type to its
// infra.* IaC successor (issue #617).
var ModuleTypes = map[string]string{
	"platform.do_app":        "infra.container_service",
	"platform.do_database":   "infra.database",
	"platform.do_dns":        "infra.dns",
	"platform.do_networking": "infra.vpc + infra.firewall",
	"platform.doks":          "infra.k8s_cluster",
}

// StepTypes maps each removed legacy DigitalOcean step type to its
// successor or to a workaround when no 1:1 successor exists.
var StepTypes = map[string]string{
	"step.do_deploy":  "step.iac_apply (against an infra.container_service module)",
	"step.do_status":  "step.iac_status (against an infra.container_service module)",
	"step.do_destroy": "step.iac_destroy (against an infra.container_service module)",
	"step.do_logs":    "no direct pipeline-step equivalent; use `wfctl infra logs` ad-hoc, or rely on the DO plugin's Troubleshoot hook on step.iac_apply failure",
	"step.do_scale":   "no direct pipeline-step equivalent; update instance_count in the infra.container_service module config and re-run step.iac_apply",
}

// IsModuleType reports whether t is a removed legacy DO module type.
func IsModuleType(t string) bool { _, ok := ModuleTypes[t]; return ok }

// IsStepType reports whether t is a removed legacy DO step type.
func IsStepType(t string) bool { _, ok := StepTypes[t]; return ok }

// FormatModuleError builds the actionable migration error for a legacy
// DO module type. iacProviderLoaded indicates whether the iac.provider factory
// is registered in the engine — used to branch between the "install plugin"
// and "config-only issue" messages.
func FormatModuleError(legacyType, moduleName string, iacProviderLoaded bool) error {
	successor, ok := ModuleTypes[legacyType]
	if !ok {
		return nil
	}
	pluginLine := "Install workflow-plugin-digitalocean: https://github.com/GoCodeAlone/workflow-plugin-digitalocean"
	if iacProviderLoaded {
		pluginLine = "workflow-plugin-digitalocean is already loaded; your config still references the legacy module name."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "unsupported legacy module type %q (module %q): this type was removed from workflow core in %s — DigitalOcean IaC moved to workflow-plugin-digitalocean.\n\n", legacyType, moduleName, RemovedInVersion)
	b.WriteString(pluginLine)
	b.WriteString("\n\nMigrate this module to: ")
	b.WriteString(successor)
	b.WriteString(" (provider: digitalocean)\n\nFull mapping:\n")
	keys := make([]string, 0, len(ModuleTypes))
	for k := range ModuleTypes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&b, "  %s → %s\n", k, ModuleTypes[k])
	}
	b.WriteString("\nSee docs/migrations/v0.52.0-godo-removal.md")
	return fmt.Errorf("%s", b.String())
}

// FormatStepError builds the actionable migration error for a legacy
// DO step type.
func FormatStepError(legacyType string, iacProviderLoaded bool) error {
	successor, ok := StepTypes[legacyType]
	if !ok {
		return nil
	}
	pluginLine := "Install workflow-plugin-digitalocean: https://github.com/GoCodeAlone/workflow-plugin-digitalocean"
	if iacProviderLoaded {
		pluginLine = "workflow-plugin-digitalocean is already loaded; your config still references the legacy step name."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "unsupported legacy step type %q: this step was removed from workflow core in %s — DigitalOcean IaC moved to workflow-plugin-digitalocean.\n\n", legacyType, RemovedInVersion)
	b.WriteString(pluginLine)
	b.WriteString("\n\nMigrate this step to: ")
	b.WriteString(successor)
	b.WriteString("\n\nSee docs/migrations/v0.52.0-godo-removal.md")
	return fmt.Errorf("%s", b.String())
}
