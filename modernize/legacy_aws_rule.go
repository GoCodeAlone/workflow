package modernize

import (
	"fmt"

	"github.com/GoCodeAlone/workflow/internal/legacyaws"
	"gopkg.in/yaml.v3"
)

// legacyAWSRule flags legacy AWS module + step types and rewrites
// module types to their infra.* IaC successors (issue #653).
//
// Auto-fixable: 3 of 4 modules (platform.ecs/apigateway/autoscaling).
// Not auto-fixable: platform.networking (1→2 split: infra.vpc + infra.firewall).
// Step types: flagged but NOT auto-rewritten — step.iac_apply/status/destroy
// require different config keys (platform + state_store) vs the legacy module/
// service keys. Operator must rewrite step config manually per the migration
// guide (docs/migrations/v0.53.0-aws-iac-removal.md).
func legacyAWSRule() Rule {
	moduleMap := map[string]string{
		"platform.ecs":         "infra.container_service",
		"platform.apigateway":  "infra.api_gateway",
		"platform.autoscaling": "infra.autoscaling_group",
		// platform.networking is intentionally NOT auto-fixed: it splits
		// 1→2 (infra.vpc + infra.firewall), which requires a structural
		// rewrite the operator must review.
	}
	// stepMap: successor name shown in migration messages only — NOT auto-fixed.
	stepMap := map[string]string{
		"step.ecs_plan":        "step.iac_plan",
		"step.ecs_apply":       "step.iac_apply",
		"step.ecs_status":      "step.iac_status",
		"step.ecs_destroy":     "step.iac_destroy",
		"step.network_plan":    "step.iac_plan",
		"step.network_apply":   "step.iac_apply",
		"step.network_status":  "step.iac_status",
		"step.apigw_plan":      "step.iac_plan",
		"step.apigw_apply":     "step.iac_apply",
		"step.apigw_status":    "step.iac_status",
		"step.apigw_destroy":   "step.iac_destroy",
		"step.scaling_plan":    "step.iac_plan",
		"step.scaling_apply":   "step.iac_apply",
		"step.scaling_status":  "step.iac_status",
		"step.scaling_destroy": "step.iac_destroy",
	}
	gapTypes := map[string]string{
		"platform.networking": "splits into infra.vpc + infra.firewall — manual rewrite required",
	}

	return Rule{
		ID:          "legacy-aws-types",
		Description: "Rewrite legacy AWS module/step types to infra.* IaC successors (issue #653).",
		Severity:    "error",
		Check: func(root *yaml.Node, raw []byte) []Finding {
			var out []Finding
			walkTypeNodes(root, func(typeVal *yaml.Node) {
				if successor, ok := moduleMap[typeVal.Value]; ok {
					out = append(out, Finding{
						RuleID:  "legacy-aws-types",
						Line:    typeVal.Line,
						Message: fmt.Sprintf("%s removed in %s; rewrite to %s (provider: aws) — requires workflow-plugin-aws v0.2.0+", typeVal.Value, legacyaws.RemovedInVersion, successor),
						Fixable: true,
					})
				}
				if successor, ok := stepMap[typeVal.Value]; ok {
					out = append(out, Finding{
						RuleID: "legacy-aws-types",
						Line:   typeVal.Line,
						// Not auto-fixable: step.iac_apply/status/destroy require
						// different config keys (platform + state_store) vs legacy
						// service/gateway/scaling keys. Auto-rewriting the type
						// alone would produce an invalid config.
						Message: fmt.Sprintf("%s removed in %s; manually rewrite to %s with config keys platform + state_store (see docs/migrations/v0.53.0-aws-iac-removal.md) — requires workflow-plugin-aws v0.2.0+", typeVal.Value, legacyaws.RemovedInVersion, successor),
						Fixable: false,
					})
				}
				if reason, ok := gapTypes[typeVal.Value]; ok {
					out = append(out, Finding{
						RuleID:  "legacy-aws-types",
						Line:    typeVal.Line,
						Message: fmt.Sprintf("%s removed in %s — %s", typeVal.Value, legacyaws.RemovedInVersion, reason),
						Fixable: false,
					})
				}
			})
			return out
		},
		Fix: func(root *yaml.Node) []Change {
			var out []Change
			walkTypeNodes(root, func(typeVal *yaml.Node) {
				if successor, ok := moduleMap[typeVal.Value]; ok {
					old := typeVal.Value
					typeVal.Value = successor
					out = append(out, Change{
						RuleID:      "legacy-aws-types",
						Line:        typeVal.Line,
						Description: fmt.Sprintf("rewrote %s → %s", old, successor),
					})
				}
				// stepMap and gapTypes are intentionally NOT rewritten.
			})
			return out
		},
	}
}
