package modernize

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestLegacyAWSRule_Rewrites(t *testing.T) {
	cases := []struct {
		name     string
		yamlIn   string
		wantNew  string // must appear in fixed YAML
		wantDrop string // must NOT appear in fixed YAML (the legacy type)
	}{
		{
			name:     "platform.ecs → infra.container_service (provider NOT auto-injected)",
			yamlIn:   "modules:\n  - name: svc\n    type: platform.ecs\n    config:\n      cluster: my-cluster\n",
			wantNew:  "infra.container_service",
			wantDrop: "platform.ecs",
		},
		{
			name:     "platform.apigateway → infra.api_gateway",
			yamlIn:   "modules:\n  - name: gw\n    type: platform.apigateway\n    config: {}\n",
			wantNew:  "infra.api_gateway",
			wantDrop: "platform.apigateway",
		},
		{
			name:     "platform.autoscaling → infra.autoscaling_group",
			yamlIn:   "modules:\n  - name: asg\n    type: platform.autoscaling\n    config: {}\n",
			wantNew:  "infra.autoscaling_group",
			wantDrop: "platform.autoscaling",
		},
	}
	rule := legacyAWSRule()
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

func TestLegacyAWSRule_GapTypesFlaggedNotRewritten(t *testing.T) {
	// Non-fixable types: the rule must flag them as findings (Fixable: false)
	// and must NOT modify the YAML after Fix() runs.
	cases := []struct {
		name   string
		legacy string
		yamlIn string
	}{
		// platform.networking: splits 1→2 (infra.vpc + infra.firewall)
		{"platform.networking", "platform.networking", "modules:\n  - name: net\n    type: platform.networking\n    config: {}\n"},
		// Step types: config key shape mismatch — not auto-fixable
		{"step.ecs_apply", "step.ecs_apply", "pipelines:\n  - steps:\n      - type: step.ecs_apply\n        config:\n          service: svc\n"},
		{"step.ecs_plan", "step.ecs_plan", "pipelines:\n  - steps:\n      - type: step.ecs_plan\n        config:\n          service: svc\n"},
		{"step.ecs_status", "step.ecs_status", "pipelines:\n  - steps:\n      - type: step.ecs_status\n        config:\n          service: svc\n"},
		{"step.ecs_destroy", "step.ecs_destroy", "pipelines:\n  - steps:\n      - type: step.ecs_destroy\n        config:\n          service: svc\n"},
		{"step.apigw_apply", "step.apigw_apply", "pipelines:\n  - steps:\n      - type: step.apigw_apply\n        config:\n          gateway: gw\n"},
		{"step.scaling_apply", "step.scaling_apply", "pipelines:\n  - steps:\n      - type: step.scaling_apply\n        config:\n          scaling: asg\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var root yaml.Node
			if err := yaml.Unmarshal([]byte(tc.yamlIn), &root); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			rule := legacyAWSRule()
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
