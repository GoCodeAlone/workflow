package modernize

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestLegacyDORule_Rewrites(t *testing.T) {
	cases := []struct {
		name     string
		yamlIn   string
		wantNew  string // must appear in fixed YAML
		wantDrop string // must NOT appear in fixed YAML (the legacy type)
	}{
		{
			name:     "platform.do_app → infra.container_service (provider NOT auto-injected)",
			yamlIn:   "modules:\n  - name: api\n    type: platform.do_app\n    config:\n      region: nyc\n",
			wantNew:  "infra.container_service",
			wantDrop: "platform.do_app",
		},
		{
			name:     "platform.do_database → infra.database",
			yamlIn:   "modules:\n  - name: db\n    type: platform.do_database\n    config: {}\n",
			wantNew:  "infra.database",
			wantDrop: "platform.do_database",
		},
		{
			name:     "platform.do_dns → infra.dns",
			yamlIn:   "modules:\n  - name: dns\n    type: platform.do_dns\n    config: {}\n",
			wantNew:  "infra.dns",
			wantDrop: "platform.do_dns",
		},
		{
			name:     "platform.doks → infra.k8s_cluster",
			yamlIn:   "modules:\n  - name: k8s\n    type: platform.doks\n    config: {}\n",
			wantNew:  "infra.k8s_cluster",
			wantDrop: "platform.doks",
		},
	}
	rule := legacyDORule()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var root yaml.Node
			if err := yaml.Unmarshal([]byte(tc.yamlIn), &root); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			findings := rule.Check(&root, []byte(tc.yamlIn))
			if len(findings) == 0 {
				t.Fatalf("expected a finding, got 0")
			}
			rule.Fix(&root)
			out, err := yaml.Marshal(&root)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			s := string(out)
			if !strings.Contains(s, tc.wantNew) {
				t.Errorf("fixed YAML missing %q; got:\n%s", tc.wantNew, s)
			}
			if strings.Contains(s, tc.wantDrop) {
				t.Errorf("fixed YAML still contains legacy %q; got:\n%s", tc.wantDrop, s)
			}
		})
	}
}

func TestLegacyDORule_GapTypesFlaggedNotRewritten(t *testing.T) {
	// Non-fixable types: the rule must flag them as findings (Fixable: false)
	// and must NOT modify the YAML after Fix() runs.
	//
	// Includes:
	//   - step.do_logs/scale: no 1:1 pipeline-step successor (GAP types).
	//   - platform.do_networking: splits 1→2, manual rewrite required.
	//   - step.do_deploy/status/destroy: step.iac_apply/status/destroy require
	//     different config keys (platform + state_store vs legacy app:), so
	//     auto-rewriting the type alone produces an invalid config.
	cases := []struct {
		name   string
		legacy string
		yamlIn string
	}{
		{"step.do_logs", "step.do_logs", "pipelines:\n  - steps:\n      - type: step.do_logs\n"},
		{"step.do_scale", "step.do_scale", "pipelines:\n  - steps:\n      - type: step.do_scale\n"},
		{"platform.do_networking", "platform.do_networking", "modules:\n  - name: net\n    type: platform.do_networking\n    config: {}\n"},
		{"step.do_deploy", "step.do_deploy", "pipelines:\n  - steps:\n      - type: step.do_deploy\n        config:\n          app: api\n"},
		{"step.do_status", "step.do_status", "pipelines:\n  - steps:\n      - type: step.do_status\n        config:\n          app: api\n"},
		{"step.do_destroy", "step.do_destroy", "pipelines:\n  - steps:\n      - type: step.do_destroy\n        config:\n          app: api\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var root yaml.Node
			if err := yaml.Unmarshal([]byte(tc.yamlIn), &root); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			rule := legacyDORule()
			findings := rule.Check(&root, []byte(tc.yamlIn))
			if len(findings) == 0 {
				t.Fatalf("expected a finding for %q", tc.legacy)
			}
			if findings[0].Fixable {
				t.Errorf("%q must be marked Fixable: false (no auto-rewrite); got Fixable: true", tc.legacy)
			}
			rule.Fix(&root)
			out, _ := yaml.Marshal(&root)
			if !strings.Contains(string(out), tc.legacy) {
				t.Errorf("Fix MUST NOT remove legacy %q; got:\n%s", tc.legacy, out)
			}
		})
	}
}
