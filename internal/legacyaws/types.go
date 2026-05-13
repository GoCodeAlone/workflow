// Package legacyaws holds the read-only data and message formatters for the
// legacy AWS module + step types removed in issue #653. Lives in internal/
// so that both module/ and modernize/ can import it without a cycle (module
// transitively imports modernize via plugin, so modernize cannot import module).
package legacyaws

import (
	"fmt"
	"sort"
	"strings"
)

// RemovedInVersion is the workflow tag that ships issue #653's force-cutover.
// Used in every legacy-AWS migration error and in the wfctl modernize rule.
const RemovedInVersion = "v0.53.0"

// ModuleTypes maps each removed legacy AWS module type to its infra.*
// IaC successor (issue #653).
var ModuleTypes = map[string]string{
	"platform.ecs":         "infra.container_service",
	"platform.networking":  "infra.vpc + infra.firewall",
	"platform.apigateway":  "infra.api_gateway",
	"platform.autoscaling": "infra.autoscaling_group",
}

// StepTypes maps each removed legacy AWS step type to its successor.
var StepTypes = map[string]string{
	"step.ecs_plan":        "step.iac_plan (against an infra.container_service module); required config keys: platform (iac.provider service name) + state_store (IaC state backend module name)",
	"step.ecs_apply":       "step.iac_apply (against an infra.container_service module); required config keys: platform (iac.provider service name) + state_store (IaC state backend module name)",
	"step.ecs_status":      "step.iac_status (against an infra.container_service module); required config keys: platform (iac.provider service name) + state_store (IaC state backend module name)",
	"step.ecs_destroy":     "step.iac_destroy (against an infra.container_service module); required config keys: platform (iac.provider service name) + state_store (IaC state backend module name)",
	"step.network_plan":    "step.iac_plan (against an infra.vpc module); required config keys: platform (iac.provider service name) + state_store (IaC state backend module name)",
	"step.network_apply":   "step.iac_apply (against an infra.vpc module); required config keys: platform (iac.provider service name) + state_store (IaC state backend module name)",
	"step.network_status":  "step.iac_status (against an infra.vpc module); required config keys: platform (iac.provider service name) + state_store (IaC state backend module name)",
	"step.apigw_plan":      "step.iac_plan (against an infra.api_gateway module); required config keys: platform (iac.provider service name) + state_store (IaC state backend module name)",
	"step.apigw_apply":     "step.iac_apply (against an infra.api_gateway module); required config keys: platform (iac.provider service name) + state_store (IaC state backend module name)",
	"step.apigw_status":    "step.iac_status (against an infra.api_gateway module); required config keys: platform (iac.provider service name) + state_store (IaC state backend module name)",
	"step.apigw_destroy":   "step.iac_destroy (against an infra.api_gateway module); required config keys: platform (iac.provider service name) + state_store (IaC state backend module name)",
	"step.scaling_plan":    "step.iac_plan (against an infra.autoscaling_group module); required config keys: platform (iac.provider service name) + state_store (IaC state backend module name)",
	"step.scaling_apply":   "step.iac_apply (against an infra.autoscaling_group module); required config keys: platform (iac.provider service name) + state_store (IaC state backend module name)",
	"step.scaling_status":  "step.iac_status (against an infra.autoscaling_group module); required config keys: platform (iac.provider service name) + state_store (IaC state backend module name)",
	"step.scaling_destroy": "step.iac_destroy (against an infra.autoscaling_group module); required config keys: platform (iac.provider service name) + state_store (IaC state backend module name)",
}

// IsModuleType reports whether t is a removed legacy AWS module type.
func IsModuleType(t string) bool { _, ok := ModuleTypes[t]; return ok }

// IsStepType reports whether t is a removed legacy AWS step type.
func IsStepType(t string) bool { _, ok := StepTypes[t]; return ok }

// FormatModuleError builds the actionable migration error for a legacy
// AWS module type. iacProviderLoaded indicates whether the iac.provider factory
// is registered in the engine — used to branch between the "install plugin"
// and "config-only issue" messages.
func FormatModuleError(legacyType, moduleName string, iacProviderLoaded bool) error {
	successor, ok := ModuleTypes[legacyType]
	if !ok {
		return nil
	}
	pluginLine := "Install workflow-plugin-aws v0.2.0+: https://github.com/GoCodeAlone/workflow-plugin-aws"
	if iacProviderLoaded {
		pluginLine = "workflow-plugin-aws is already loaded; your config still references the legacy module name."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "unsupported legacy module type %q (module %q): this type was removed from workflow core in %s — AWS IaC moved to workflow-plugin-aws.\n\n", legacyType, moduleName, RemovedInVersion)
	b.WriteString(pluginLine)
	b.WriteString("\n\nMigrate this module to: ")
	b.WriteString(successor)
	b.WriteString(" (provider: aws)\n\nFull mapping:\n")
	keys := make([]string, 0, len(ModuleTypes))
	for k := range ModuleTypes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&b, "  %s → %s\n", k, ModuleTypes[k])
	}
	b.WriteString("\nSee docs/migrations/v0.53.0-aws-iac-removal.md")
	return fmt.Errorf("%s", b.String())
}

// FormatStepError builds the actionable migration error for a legacy
// AWS step type.
func FormatStepError(legacyType string, iacProviderLoaded bool) error {
	successor, ok := StepTypes[legacyType]
	if !ok {
		return nil
	}
	pluginLine := "Install workflow-plugin-aws v0.2.0+: https://github.com/GoCodeAlone/workflow-plugin-aws"
	if iacProviderLoaded {
		pluginLine = "workflow-plugin-aws is already loaded; your config still references the legacy step name."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "unsupported legacy step type %q: this step was removed from workflow core in %s — AWS IaC moved to workflow-plugin-aws.\n\n", legacyType, RemovedInVersion)
	b.WriteString(pluginLine)
	b.WriteString("\n\nMigrate this step to: ")
	b.WriteString(successor)
	b.WriteString("\n\nSee docs/migrations/v0.53.0-aws-iac-removal.md")
	return fmt.Errorf("%s", b.String())
}
