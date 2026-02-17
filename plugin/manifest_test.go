package plugin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/dynamic"
)

func TestSemverParse(t *testing.T) {
	tests := []struct {
		input   string
		want    Semver
		wantErr bool
	}{
		{"1.2.3", Semver{1, 2, 3}, false},
		{"v1.2.3", Semver{1, 2, 3}, false},
		{"0.0.0", Semver{0, 0, 0}, false},
		{"10.20.30", Semver{10, 20, 30}, false},
		{"1.2", Semver{}, true},
		{"abc", Semver{}, true},
		{"1.2.abc", Semver{}, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseSemver(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseSemver(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("ParseSemver(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestSemverCompare(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.0.1", "1.0.0", 1},
		{"1.0.0", "1.0.1", -1},
		{"2.0.0", "1.9.9", 1},
		{"1.2.0", "1.1.9", 1},
	}

	for _, tt := range tests {
		t.Run(tt.a+"_vs_"+tt.b, func(t *testing.T) {
			a, _ := ParseSemver(tt.a)
			b, _ := ParseSemver(tt.b)
			got := a.Compare(b)
			if got != tt.want {
				t.Errorf("(%s).Compare(%s) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestSemverString(t *testing.T) {
	s := Semver{1, 2, 3}
	if s.String() != "1.2.3" {
		t.Errorf("Semver.String() = %q, want %q", s.String(), "1.2.3")
	}
}

func TestConstraintParse(t *testing.T) {
	tests := []struct {
		input   string
		wantOp  string
		wantVer string
		wantErr bool
	}{
		{">=1.0.0", ">=", "1.0.0", false},
		{"^2.1.0", "^", "2.1.0", false},
		{"~1.2.0", "~", "1.2.0", false},
		{">1.0.0", ">", "1.0.0", false},
		{"<1.0.0", "<", "1.0.0", false},
		{"<=1.0.0", "<=", "1.0.0", false},
		{"!=1.0.0", "!=", "1.0.0", false},
		{"=1.0.0", "=", "1.0.0", false},
		{"1.0.0", "=", "1.0.0", false},
		{"", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			c, err := ParseConstraint(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseConstraint(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if c.Op != tt.wantOp {
				t.Errorf("Op = %q, want %q", c.Op, tt.wantOp)
			}
			if c.Version.String() != tt.wantVer {
				t.Errorf("Version = %s, want %s", c.Version.String(), tt.wantVer)
			}
		})
	}
}

func TestConstraintCheck(t *testing.T) {
	tests := []struct {
		constraint string
		version    string
		want       bool
	}{
		{">=1.0.0", "1.0.0", true},
		{">=1.0.0", "2.0.0", true},
		{">=1.0.0", "0.9.0", false},
		{">1.0.0", "1.0.1", true},
		{">1.0.0", "1.0.0", false},
		{"<2.0.0", "1.9.9", true},
		{"<2.0.0", "2.0.0", false},
		{"<=2.0.0", "2.0.0", true},
		{"!=1.0.0", "1.0.1", true},
		{"!=1.0.0", "1.0.0", false},
		{"=1.0.0", "1.0.0", true},
		{"=1.0.0", "1.0.1", false},
		{"^1.0.0", "1.5.0", true},
		{"^1.0.0", "1.0.0", true},
		{"^1.0.0", "2.0.0", false},
		{"^1.0.0", "0.9.0", false},
		{"~1.2.0", "1.2.5", true},
		{"~1.2.0", "1.2.0", true},
		{"~1.2.0", "1.3.0", false},
		{"~1.2.0", "1.1.0", false},
	}

	for _, tt := range tests {
		t.Run(tt.constraint+"_"+tt.version, func(t *testing.T) {
			got, err := CheckVersion(tt.version, tt.constraint)
			if err != nil {
				t.Fatalf("CheckVersion(%q, %q) error = %v", tt.version, tt.constraint, err)
			}
			if got != tt.want {
				t.Errorf("CheckVersion(%q, %q) = %v, want %v", tt.version, tt.constraint, got, tt.want)
			}
		})
	}
}

func TestCheckVersionErrors(t *testing.T) {
	_, err := CheckVersion("bad", ">=1.0.0")
	if err == nil {
		t.Error("expected error for invalid version")
	}
	_, err = CheckVersion("1.0.0", ">>bad")
	if err == nil {
		t.Error("expected error for invalid constraint")
	}
}

func TestManifestValidate(t *testing.T) {
	valid := &PluginManifest{
		Name:        "my-plugin",
		Version:     "1.0.0",
		Author:      "Test Author",
		Description: "A test plugin",
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("expected valid manifest, got error: %v", err)
	}

	tests := []struct {
		name   string
		modify func(m *PluginManifest)
	}{
		{"missing name", func(m *PluginManifest) { m.Name = "" }},
		{"invalid name", func(m *PluginManifest) { m.Name = "Invalid_Name" }},
		{"missing version", func(m *PluginManifest) { m.Version = "" }},
		{"invalid version", func(m *PluginManifest) { m.Version = "not-a-version" }},
		{"missing author", func(m *PluginManifest) { m.Author = "" }},
		{"missing description", func(m *PluginManifest) { m.Description = "" }},
		{"invalid dep constraint", func(m *PluginManifest) {
			m.Dependencies = []Dependency{{Name: "dep", Constraint: ">>>bad"}}
		}},
		{"missing dep name", func(m *PluginManifest) {
			m.Dependencies = []Dependency{{Name: "", Constraint: ">=1.0.0"}}
		}},
		{"missing dep constraint", func(m *PluginManifest) {
			m.Dependencies = []Dependency{{Name: "dep", Constraint: ""}}
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &PluginManifest{
				Name:        "my-plugin",
				Version:     "1.0.0",
				Author:      "Test Author",
				Description: "A test plugin",
			}
			tt.modify(m)
			if err := m.Validate(); err == nil {
				t.Error("expected validation error")
			}
		})
	}
}

func TestManifestValidateWithContract(t *testing.T) {
	m := &PluginManifest{
		Name:        "contract-plugin",
		Version:     "1.0.0",
		Author:      "Author",
		Description: "With contract",
		Contract: &dynamic.FieldContract{
			RequiredInputs: map[string]dynamic.FieldSpec{
				"input": {Type: dynamic.FieldTypeString, Description: "test"},
			},
		},
	}
	if err := m.Validate(); err != nil {
		t.Fatalf("expected valid manifest with contract, got: %v", err)
	}
}

func TestPluginNameValidation(t *testing.T) {
	tests := []struct {
		name  string
		valid bool
	}{
		{"a", true},
		{"ab", true},
		{"my-plugin", true},
		{"my-plugin-2", true},
		{"a1", true},
		{"", false},
		{"-bad", false},
		{"bad-", false},
		{"Bad", false},
		{"my_plugin", false},
		{"my plugin", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isValidPluginName(tt.name)
			if got != tt.valid {
				t.Errorf("isValidPluginName(%q) = %v, want %v", tt.name, got, tt.valid)
			}
		})
	}
}

func TestLoadManifest(t *testing.T) {
	dir := t.TempDir()
	m := &PluginManifest{
		Name:        "test-plugin",
		Version:     "1.0.0",
		Author:      "Test",
		Description: "Test plugin",
	}
	data, _ := json.MarshalIndent(m, "", "  ")
	path := filepath.Join(dir, "plugin.json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadManifest(path)
	if err != nil {
		t.Fatalf("LoadManifest error: %v", err)
	}
	if loaded.Name != m.Name {
		t.Errorf("Name = %q, want %q", loaded.Name, m.Name)
	}
	if loaded.Version != m.Version {
		t.Errorf("Version = %q, want %q", loaded.Version, m.Version)
	}
}

func TestLoadManifestNotFound(t *testing.T) {
	_, err := LoadManifest("/nonexistent/path")
	if err == nil {
		t.Error("expected error for nonexistent path")
	}
}

func TestLoadManifestInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plugin.json")
	if err := os.WriteFile(path, []byte("not json"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadManifest(path)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestManifestEngineFieldsRoundTrip(t *testing.T) {
	m := &PluginManifest{
		Name:        "engine-plugin",
		Version:     "2.0.0",
		Author:      "Test",
		Description: "Engine plugin with all fields",
		Capabilities: []CapabilityDecl{
			{Name: "http-server", Role: "provider", Priority: 10},
			{Name: "message-broker", Role: "consumer"},
		},
		ModuleTypes:   []string{"http.server", "http.client"},
		StepTypes:     []string{"step.validate", "step.transform"},
		TriggerTypes:  []string{"http", "cron"},
		WorkflowTypes: []string{"http", "messaging"},
		WiringHooks:   []string{"wire-metrics", "wire-logging"},
	}

	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var loaded PluginManifest
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Capabilities
	if len(loaded.Capabilities) != 2 {
		t.Fatalf("Capabilities len = %d, want 2", len(loaded.Capabilities))
	}
	if loaded.Capabilities[0].Name != "http-server" {
		t.Errorf("Capabilities[0].Name = %q, want %q", loaded.Capabilities[0].Name, "http-server")
	}
	if loaded.Capabilities[0].Role != "provider" {
		t.Errorf("Capabilities[0].Role = %q, want %q", loaded.Capabilities[0].Role, "provider")
	}
	if loaded.Capabilities[0].Priority != 10 {
		t.Errorf("Capabilities[0].Priority = %d, want %d", loaded.Capabilities[0].Priority, 10)
	}
	if loaded.Capabilities[1].Name != "message-broker" {
		t.Errorf("Capabilities[1].Name = %q, want %q", loaded.Capabilities[1].Name, "message-broker")
	}
	if loaded.Capabilities[1].Priority != 0 {
		t.Errorf("Capabilities[1].Priority = %d, want %d", loaded.Capabilities[1].Priority, 0)
	}

	// ModuleTypes
	if len(loaded.ModuleTypes) != 2 || loaded.ModuleTypes[0] != "http.server" || loaded.ModuleTypes[1] != "http.client" {
		t.Errorf("ModuleTypes = %v, want [http.server http.client]", loaded.ModuleTypes)
	}

	// StepTypes
	if len(loaded.StepTypes) != 2 || loaded.StepTypes[0] != "step.validate" {
		t.Errorf("StepTypes = %v, want [step.validate step.transform]", loaded.StepTypes)
	}

	// TriggerTypes
	if len(loaded.TriggerTypes) != 2 || loaded.TriggerTypes[0] != "http" {
		t.Errorf("TriggerTypes = %v, want [http cron]", loaded.TriggerTypes)
	}

	// WorkflowTypes
	if len(loaded.WorkflowTypes) != 2 || loaded.WorkflowTypes[0] != "http" {
		t.Errorf("WorkflowTypes = %v, want [http messaging]", loaded.WorkflowTypes)
	}

	// WiringHooks
	if len(loaded.WiringHooks) != 2 || loaded.WiringHooks[0] != "wire-metrics" {
		t.Errorf("WiringHooks = %v, want [wire-metrics wire-logging]", loaded.WiringHooks)
	}
}

func TestManifestEngineFieldsOmitEmpty(t *testing.T) {
	m := &PluginManifest{
		Name:        "basic-plugin",
		Version:     "1.0.0",
		Author:      "Test",
		Description: "No engine fields",
	}

	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	raw := string(data)
	for _, field := range []string{"capabilities", "moduleTypes", "stepTypes", "triggerTypes", "workflowTypes", "wiringHooks"} {
		if strings.Contains(raw, field) {
			t.Errorf("expected field %q to be omitted from JSON when empty, got: %s", field, raw)
		}
	}
}

func TestManifestEngineFieldsLoadFromFile(t *testing.T) {
	dir := t.TempDir()
	m := &PluginManifest{
		Name:         "file-engine-plugin",
		Version:      "1.0.0",
		Author:       "Test",
		Description:  "Test file load with engine fields",
		ModuleTypes:  []string{"custom.module"},
		TriggerTypes: []string{"custom.trigger"},
		Capabilities: []CapabilityDecl{
			{Name: "storage", Role: "provider", Priority: 5},
		},
	}

	data, _ := json.MarshalIndent(m, "", "  ")
	path := filepath.Join(dir, "plugin.json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadManifest(path)
	if err != nil {
		t.Fatalf("LoadManifest error: %v", err)
	}

	if len(loaded.ModuleTypes) != 1 || loaded.ModuleTypes[0] != "custom.module" {
		t.Errorf("ModuleTypes = %v, want [custom.module]", loaded.ModuleTypes)
	}
	if len(loaded.TriggerTypes) != 1 || loaded.TriggerTypes[0] != "custom.trigger" {
		t.Errorf("TriggerTypes = %v, want [custom.trigger]", loaded.TriggerTypes)
	}
	if len(loaded.Capabilities) != 1 || loaded.Capabilities[0].Name != "storage" {
		t.Errorf("Capabilities = %v, want [{storage provider 5}]", loaded.Capabilities)
	}
}
