package modernize

import (
	"fmt"

	"github.com/GoCodeAlone/workflow/internal/legacydo"
	"gopkg.in/yaml.v3"
)

// Import note: `modernize` MUST NOT import `module` directly. `module`
// transitively imports `modernize` via `plugin` (plugin/manifest.go +
// plugin/engine_plugin.go), so `modernize → module` creates an import cycle.
// Shared constants live in `internal/legacydo`, a leaf package that imports
// only stdlib and is safe for both `module` and `modernize` to consume.

// legacyDORule rewrites legacy DigitalOcean module + step types to their
// infra.* IaC successors (issue #617).
//
// IMPORTANT: The Fix function ONLY renames the `type:` key — it does NOT
// inject the required `config.provider: digitalocean` setting, because that
// requires modifying a sibling mapping that may already contain unrelated
// keys the operator must review. The rule's Check Message and the migration
// guide both instruct the operator to add the provider key manually after
// running modernize. The committed `testdata/legacy-do-config.expected.yaml`
// fixture asserts the post-modernize shape: types renamed, provider NOT
// auto-added. Adding provider injection in a future iteration is tracked as
// a follow-up (see migration guide).
//
// Auto-fixable for 4 of 5 modules (platform.do_app/database/dns/doks) and
// 3 of 5 steps (step.do_deploy/status/destroy). The GAP types (do_networking
// splits 1→2; step.do_logs/scale have no pipeline-step successor) are flagged
// but not modified.
func legacyDORule() Rule {
	moduleMap := map[string]string{
		"platform.do_app":      "infra.container_service",
		"platform.do_database": "infra.database",
		"platform.do_dns":      "infra.dns",
		"platform.doks":        "infra.k8s_cluster",
		// platform.do_networking is intentionally NOT auto-fixed: it splits
		// 1→2 (infra.vpc + infra.firewall), which requires structural
		// rewrite the operator must review.
	}
	stepMap := map[string]string{
		"step.do_deploy":  "step.iac_apply",
		"step.do_status":  "step.iac_status",
		"step.do_destroy": "step.iac_destroy",
	}
	gapTypes := map[string]string{
		"platform.do_networking": "splits into infra.vpc + infra.firewall — manual rewrite required",
		"step.do_logs":           "no pipeline-step successor; use `wfctl infra logs` or rely on DO plugin Troubleshoot",
		"step.do_scale":          "no pipeline-step successor; edit instance_count and re-run step.iac_apply",
	}

	return Rule{
		ID:          "legacy-do-types",
		Description: "Rewrite legacy DigitalOcean module/step types to infra.* IaC successors (issue #617).",
		Severity:    "error",
		Check: func(root *yaml.Node, raw []byte) []Finding {
			var out []Finding
			walkTypeNodes(root, func(typeVal *yaml.Node) {
				if successor, ok := moduleMap[typeVal.Value]; ok {
					out = append(out, Finding{
						RuleID:  "legacy-do-types",
						Line:    typeVal.Line,
						Message: fmt.Sprintf("%s removed in %s; rewrite to %s (provider: digitalocean) — requires workflow-plugin-digitalocean", typeVal.Value, legacydo.RemovedInVersion, successor),
						Fixable: true,
					})
				}
				if successor, ok := stepMap[typeVal.Value]; ok {
					out = append(out, Finding{
						RuleID:  "legacy-do-types",
						Line:    typeVal.Line,
						Message: fmt.Sprintf("%s removed in %s; rewrite to %s — requires workflow-plugin-digitalocean", typeVal.Value, legacydo.RemovedInVersion, successor),
						Fixable: true,
					})
				}
				if reason, ok := gapTypes[typeVal.Value]; ok {
					out = append(out, Finding{
						RuleID:  "legacy-do-types",
						Line:    typeVal.Line,
						Message: fmt.Sprintf("%s removed in %s — %s", typeVal.Value, legacydo.RemovedInVersion, reason),
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
						RuleID:      "legacy-do-types",
						Line:        typeVal.Line,
						Description: fmt.Sprintf("rewrote %s → %s", old, successor),
					})
				}
				if successor, ok := stepMap[typeVal.Value]; ok {
					old := typeVal.Value
					typeVal.Value = successor
					out = append(out, Change{
						RuleID:      "legacy-do-types",
						Line:        typeVal.Line,
						Description: fmt.Sprintf("rewrote %s → %s", old, successor),
					})
				}
				// gapTypes are intentionally not modified.
			})
			return out
		},
	}
}

// walkTypeNodes traverses a YAML AST and invokes visit on every value node
// whose parent mapping key is "type". This differs from the package's existing
// walkNodes helper which visits every node — extracted as a separate helper
// because the type-key constraint produces tighter visitor code at call sites.
// If a future refactor unifies the two, prefer adding a key-filter parameter
// to walkNodes over keeping the duplication.
func walkTypeNodes(n *yaml.Node, visit func(*yaml.Node)) {
	if n == nil {
		return
	}
	if n.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(n.Content); i += 2 {
			k, v := n.Content[i], n.Content[i+1]
			if k.Value == "type" && v.Kind == yaml.ScalarNode {
				visit(v)
			}
			walkTypeNodes(v, visit)
		}
		return
	}
	for _, c := range n.Content {
		walkTypeNodes(c, visit)
	}
}
