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

// legacyDORule flags legacy DigitalOcean module + step types and rewrites
// module types to their infra.* IaC successors (issue #617).
//
// IMPORTANT: The Fix function ONLY renames the `type:` key for module types
// — it does NOT inject the required `config.provider: digitalocean` setting,
// because that requires modifying a sibling mapping that may already contain
// unrelated keys the operator must review. The rule's Check Message and the
// migration guide both instruct the operator to add the provider key manually
// after running modernize. The `testdata/legacy-do-config.expected.yaml`
// fixture documents the post-modernize shape: module types renamed, provider NOT
// auto-added, step types unchanged (non-fixable). Adding provider injection in
// a future iteration is tracked as a follow-up (see migration guide).
//
// Step types (step.do_deploy/status/destroy) are flagged but NOT auto-rewritten
// because step.iac_apply/status/destroy require different config keys
// (platform + state_store) rather than the legacy app: key. Auto-rewriting
// the type alone produces an invalid config. The operator must rewrite step
// config manually per the migration guide (docs/migrations/v0.52.0-godo-removal.md).
//
// Auto-fixable: 4 of 5 modules (platform.do_app/database/dns/doks).
// Not auto-fixable: platform.do_networking (1→2 split), all 5 step types
// (step.do_deploy/status/destroy config shape mismatch; step.do_logs/scale
// have no pipeline-step successor).
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
	// stepMap holds the successor type name for the migration error message only.
	// These findings are NOT auto-fixable: step.iac_apply/status/destroy require
	// different config keys (platform + state_store) vs the legacy app: key, so
	// rewriting type alone would produce an invalid config.
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
						RuleID: "legacy-do-types",
						Line:   typeVal.Line,
						// Fixable is false: step.iac_apply/status/destroy require
						// different config keys (platform + state_store) compared
						// to the legacy app: key. Rewriting the type alone would
						// produce an invalid config. Operator must rewrite manually.
						Message: fmt.Sprintf("%s removed in %s; manually rewrite to %s with config keys platform + state_store (see docs/migrations/v0.52.0-godo-removal.md) — requires workflow-plugin-digitalocean", typeVal.Value, legacydo.RemovedInVersion, successor),
						Fixable: false,
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
				// stepMap types are intentionally NOT rewritten: step.iac_apply/
				// status/destroy require different config keys (platform +
				// state_store) vs the legacy app: key. Auto-rewriting the type
				// alone produces an invalid config; operator must rewrite manually.
				// gapTypes are also intentionally not modified.
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
