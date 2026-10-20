package mcp

import (
	"encoding/json"
	"testing"
)

func TestMCPAnnotations(t *testing.T) {
	// Per-name effects audit: catalog reads and in-memory previews are read-only;
	// only the four path/import readers have open-ended filesystem targets.
	want := map[string][3]bool{
		"api_extract":                   {true, false, false},
		"ci_plan":                       {true, false, true},
		"compat_check":                  {true, false, false},
		"contract_generate":             {true, false, false},
		"detect_infra_needs":            {true, false, true},
		"detect_ports":                  {true, false, false},
		"detect_project_features":       {true, false, false},
		"detect_secrets":                {true, false, false},
		"diff_configs":                  {true, false, false},
		"generate_bootstrap":            {true, false, false},
		"generate_github_actions":       {true, false, true},
		"generate_schema":               {true, false, false},
		"get_config_examples":           {true, false, false},
		"get_config_skeleton":           {true, false, false},
		"get_module_schema":             {true, false, false},
		"get_step_schema":               {true, false, false},
		"get_template_functions":        {true, false, false},
		"infer_pipeline_context":        {true, false, false},
		"inspect_config":                {true, false, false},
		"list_module_types":             {true, false, false},
		"list_plugins":                  {true, false, true},
		"list_step_types":               {true, false, false},
		"list_trigger_types":            {true, false, false},
		"list_workflow_types":           {true, false, false},
		"manifest_analyze":              {true, false, false},
		"modernize":                     {true, false, false}, // apply:true only returns fixed_yaml.
		"registry_search":               {true, false, false}, // Fixed startup registry catalog.
		"scaffold_ci":                   {true, false, false},
		"scaffold_environment":          {true, false, false},
		"scaffold_infra":                {true, false, false},
		"template_validate_config":      {true, false, false},
		"validate_config":               {true, false, false},
		"validate_template_expressions": {true, false, false},
	}
	for _, engine := range []bool{false, true} {
		srv := NewServer("")
		if engine {
			srv = NewServer("", WithEngine(&mockEngine{}))
			want["run_workflow"] = [3]bool{false, true, true}
		}
		tools := srv.MCPServer().ListTools()
		if len(tools) != len(want) {
			t.Fatalf("engine=%v: got %d tools, want %d audited names", engine, len(tools), len(want))
		}
		for name, expected := range want {
			t.Run(name, func(t *testing.T) {
				registered, ok := tools[name]
				if !ok {
					t.Fatal("audited tool missing")
				}
				data, err := json.Marshal(registered.Tool)
				if err != nil {
					t.Fatal(err)
				}
				var wire struct {
					Annotations map[string]any `json:"annotations"`
				}
				if err := json.Unmarshal(data, &wire); err != nil {
					t.Fatal(err)
				}
				for i, key := range []string{"readOnlyHint", "destructiveHint", "openWorldHint"} {
					actual, present := wire.Annotations[key].(bool)
					if !present || actual != expected[i] {
						t.Errorf("engine=%v: %s=%v (boolean=%v), want %v", engine, key, wire.Annotations[key], present, expected[i])
					}
				}
			})
		}
	}
}
