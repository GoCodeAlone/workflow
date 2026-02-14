package sdk

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/plugin"
)

func TestTemplateGeneratorGenerate(t *testing.T) {
	dir := t.TempDir()
	outputDir := filepath.Join(dir, "my-plugin")

	gen := NewTemplateGenerator()
	err := gen.Generate(GenerateOptions{
		Name:        "my-plugin",
		Version:     "1.0.0",
		Author:      "Test Author",
		Description: "A test plugin",
		OutputDir:   outputDir,
	})
	if err != nil {
		t.Fatalf("Generate error: %v", err)
	}

	// Check manifest was created
	manifestPath := filepath.Join(outputDir, "plugin.json")
	manifest, err := plugin.LoadManifest(manifestPath)
	if err != nil {
		t.Fatalf("LoadManifest error: %v", err)
	}
	if manifest.Name != "my-plugin" {
		t.Errorf("Name = %q, want %q", manifest.Name, "my-plugin")
	}
	if manifest.Version != "1.0.0" {
		t.Errorf("Version = %q, want %q", manifest.Version, "1.0.0")
	}

	// Check component was created
	componentPath := filepath.Join(outputDir, "my-plugin.go")
	data, err := os.ReadFile(componentPath)
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}
	source := string(data)
	if !strings.Contains(source, "package component") {
		t.Error("expected source to contain 'package component'")
	}
	if !strings.Contains(source, `func Name() string`) {
		t.Error("expected source to contain Name function")
	}
	if !strings.Contains(source, `func Execute(`) {
		t.Error("expected source to contain Execute function")
	}
}

func TestTemplateGeneratorGenerateWithContract(t *testing.T) {
	dir := t.TempDir()
	outputDir := filepath.Join(dir, "contract-plugin")

	gen := NewTemplateGenerator()
	err := gen.Generate(GenerateOptions{
		Name:         "contract-plugin",
		Author:       "Test Author",
		Description:  "Plugin with contract",
		OutputDir:    outputDir,
		WithContract: true,
	})
	if err != nil {
		t.Fatalf("Generate error: %v", err)
	}

	// Check manifest has contract
	manifest, _ := plugin.LoadManifest(filepath.Join(outputDir, "plugin.json"))
	if manifest.Contract == nil {
		t.Error("expected manifest to have a contract")
	}
	if _, ok := manifest.Contract.RequiredInputs["input"]; !ok {
		t.Error("expected contract to have 'input' required input")
	}

	// Check component has Contract function
	data, _ := os.ReadFile(filepath.Join(outputDir, "contract-plugin.go"))
	if !strings.Contains(string(data), "func Contract()") {
		t.Error("expected source to contain Contract function")
	}
}

func TestTemplateGeneratorDefaults(t *testing.T) {
	dir := t.TempDir()
	outputDir := filepath.Join(dir, "default-plugin")

	gen := NewTemplateGenerator()
	err := gen.Generate(GenerateOptions{
		Name:        "default-plugin",
		Author:      "Author",
		Description: "Defaults test",
		OutputDir:   outputDir,
	})
	if err != nil {
		t.Fatalf("Generate error: %v", err)
	}

	manifest, _ := plugin.LoadManifest(filepath.Join(outputDir, "plugin.json"))
	if manifest.Version != "0.1.0" {
		t.Errorf("default version = %q, want %q", manifest.Version, "0.1.0")
	}
}

func TestTemplateGeneratorMissingName(t *testing.T) {
	gen := NewTemplateGenerator()
	err := gen.Generate(GenerateOptions{
		Author:      "Author",
		Description: "Test",
	})
	if err == nil {
		t.Error("expected error for missing name")
	}
}

func TestTemplateGeneratorMissingAuthor(t *testing.T) {
	gen := NewTemplateGenerator()
	err := gen.Generate(GenerateOptions{
		Name:        "test-plugin",
		Description: "Test",
		OutputDir:   t.TempDir(),
	})
	if err == nil {
		t.Error("expected error for missing author")
	}
}

func TestTemplateGeneratorInvalidName(t *testing.T) {
	gen := NewTemplateGenerator()
	err := gen.Generate(GenerateOptions{
		Name:        "Invalid_Name",
		Author:      "Author",
		Description: "Test",
		OutputDir:   t.TempDir(),
	})
	if err == nil {
		t.Error("expected error for invalid name")
	}
}

func TestToCamelCase(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"my-plugin", "MyPlugin"},
		{"simple", "Simple"},
		{"a-b-c", "ABC"},
		{"hello-world-test", "HelloWorldTest"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := toCamelCase(tt.input)
			if got != tt.want {
				t.Errorf("toCamelCase(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
