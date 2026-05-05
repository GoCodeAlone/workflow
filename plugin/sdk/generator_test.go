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

func TestTemplateGeneratorGenerateStrictContractScaffoldByDefault(t *testing.T) {
	dir := t.TempDir()
	outputDir := filepath.Join(dir, "strict-plugin")

	gen := NewTemplateGenerator()
	err := gen.Generate(GenerateOptions{
		Name:            "strict-plugin",
		Version:         "1.0.0",
		Author:          "TestOrg",
		Description:     "A strict plugin",
		OutputDir:       outputDir,
		WorkflowReplace: filepath.Join(dir, "workflow"),
	})
	if err != nil {
		t.Fatalf("Generate error: %v", err)
	}

	protoData, err := os.ReadFile(filepath.Join(outputDir, "proto", "strict_plugin.proto"))
	if err != nil {
		t.Fatalf("read proto contract: %v", err)
	}
	protoSrc := string(protoData)
	for _, want := range []string{
		`import "google/protobuf/wrappers.proto";`,
		`message ExampleStepContract`,
		`google.protobuf.StringValue config = 1;`,
		`google.protobuf.StringValue input = 2;`,
		`google.protobuf.StringValue output = 3;`,
		`option go_package = "github.com/TestOrg/workflow-plugin-strict-plugin/internal/contracts"`,
	} {
		if !strings.Contains(protoSrc, want) {
			t.Errorf("proto contract missing %q:\n%s", want, protoSrc)
		}
	}

	providerData, err := os.ReadFile(filepath.Join(outputDir, "internal", "provider.go"))
	if err != nil {
		t.Fatalf("read provider.go: %v", err)
	}
	providerSrc := string(providerData)
	for _, want := range []string{
		"ContractRegistry() *pb.ContractRegistry",
		"CONTRACT_MODE_STRICT_PROTO",
		"CreateTypedStep(",
		"TypedStepTypes() []string",
		"sdk.ErrTypedContractNotHandled",
	} {
		if !strings.Contains(providerSrc, want) {
			t.Errorf("provider.go missing %q:\n%s", want, providerSrc)
		}
	}
	if strings.Contains(providerSrc, "CreateStep(") {
		t.Errorf("provider.go should not scaffold legacy CreateStep by default:\n%s", providerSrc)
	}

	descriptorData, err := os.ReadFile(filepath.Join(outputDir, "plugin.contracts.json"))
	if err != nil {
		t.Fatalf("read plugin.contracts.json: %v", err)
	}
	descriptorSrc := string(descriptorData)
	for _, want := range []string{
		`"kind": "step"`,
		`"type": "step.strict-plugin_example"`,
		`"mode": "strict"`,
	} {
		if !strings.Contains(descriptorSrc, want) {
			t.Errorf("plugin.contracts.json missing %q:\n%s", want, descriptorSrc)
		}
	}

	stepsData, err := os.ReadFile(filepath.Join(outputDir, "internal", "steps.go"))
	if err != nil {
		t.Fatalf("read steps.go: %v", err)
	}
	stepsSrc := string(stepsData)
	for _, want := range []string{
		"sdk.TypedStepRequest",
		"sdk.TypedStepResult",
		"*contracts.StrictPluginExampleInput",
		"*contracts.StrictPluginExampleOutput",
	} {
		if !strings.Contains(stepsSrc, want) {
			t.Errorf("steps.go missing %q:\n%s", want, stepsSrc)
		}
	}
	for _, legacy := range []string{
		"current map[string]any",
		`current["input"]`,
		"config map[string]any",
		"map[string]any{",
	} {
		if strings.Contains(stepsSrc, legacy) {
			t.Errorf("steps.go should not contain legacy map entrypoint %q:\n%s", legacy, stepsSrc)
		}
	}
}

func TestTemplateGeneratorGenerateLegacyContractsOptOut(t *testing.T) {
	dir := t.TempDir()
	outputDir := filepath.Join(dir, "legacy-plugin")

	gen := NewTemplateGenerator()
	err := gen.Generate(GenerateOptions{
		Name:            "legacy-plugin",
		Author:          "TestOrg",
		Description:     "A legacy plugin",
		OutputDir:       outputDir,
		LegacyContracts: true,
	})
	if err != nil {
		t.Fatalf("Generate error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(outputDir, "proto", "legacy_plugin.proto")); !os.IsNotExist(err) {
		t.Fatalf("legacy scaffold should not include proto contract, stat err: %v", err)
	}

	stepsData, err := os.ReadFile(filepath.Join(outputDir, "internal", "steps.go"))
	if err != nil {
		t.Fatalf("read steps.go: %v", err)
	}
	if !strings.Contains(string(stepsData), "current map[string]any") {
		t.Errorf("legacy scaffold should keep map step entrypoint:\n%s", stepsData)
	}

	modData, err := os.ReadFile(filepath.Join(outputDir, "go.mod"))
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	if !strings.Contains(string(modData), "github.com/GoCodeAlone/workflow "+workflowReleasedVersion) {
		t.Fatalf("legacy scaffold should require released workflow version, got:\n%s", modData)
	}
}

func TestTemplateGeneratorFallsBackToLegacyOutsideWorkflowCheckout(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	outputDir := filepath.Join(dir, "public-plugin")
	gen := NewTemplateGenerator()
	err := gen.Generate(GenerateOptions{
		Name:        "public-plugin",
		Author:      "TestOrg",
		Description: "Public plugin",
		OutputDir:   outputDir,
	})
	if err != nil {
		t.Fatalf("Generate error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(outputDir, "plugin.contracts.json")); !os.IsNotExist(err) {
		t.Fatalf("public fallback should not include strict contract descriptors, stat err: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outputDir, "proto", "public_plugin.proto")); !os.IsNotExist(err) {
		t.Fatalf("public fallback should not include proto contract, stat err: %v", err)
	}

	stepsData, err := os.ReadFile(filepath.Join(outputDir, "internal", "steps.go"))
	if err != nil {
		t.Fatalf("read steps.go: %v", err)
	}
	if !strings.Contains(string(stepsData), "current map[string]any") {
		t.Errorf("public fallback should keep map step entrypoint:\n%s", stepsData)
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

func TestGenerateProjectStructure(t *testing.T) {
	dir := t.TempDir()
	outputDir := filepath.Join(dir, "my-plugin")

	gen := NewTemplateGenerator()
	err := gen.Generate(GenerateOptions{
		Name:            "my-plugin",
		Version:         "0.2.0",
		Author:          "TestOrg",
		Description:     "Project structure test",
		OutputDir:       outputDir,
		WorkflowReplace: filepath.Join(dir, "workflow"),
	})
	if err != nil {
		t.Fatalf("Generate error: %v", err)
	}

	// Verify all expected project files exist.
	expectedFiles := []string{
		"cmd/workflow-plugin-my-plugin/main.go",
		"internal/provider.go",
		"internal/steps.go",
		"go.mod",
		".goreleaser.yml",
		".github/workflows/ci.yml",
		".github/workflows/release.yml",
		"Makefile",
		"README.md",
	}
	for _, f := range expectedFiles {
		p := filepath.Join(outputDir, f)
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected file %s to exist: %v", f, err)
		}
	}

	// Verify main.go uses the external SDK import.
	mainData, err := os.ReadFile(filepath.Join(outputDir, "cmd/workflow-plugin-my-plugin/main.go"))
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	mainSrc := string(mainData)
	if !strings.Contains(mainSrc, `"github.com/GoCodeAlone/workflow/plugin/external/sdk"`) {
		t.Error("main.go should import plugin/external/sdk")
	}
	if !strings.Contains(mainSrc, "sdk.Serve(") {
		t.Error("main.go should call sdk.Serve()")
	}

	// Verify provider.go uses external SDK types.
	provData, err := os.ReadFile(filepath.Join(outputDir, "internal/provider.go"))
	if err != nil {
		t.Fatalf("read provider.go: %v", err)
	}
	provSrc := string(provData)
	if !strings.Contains(provSrc, `"github.com/GoCodeAlone/workflow/plugin/external/sdk"`) {
		t.Error("provider.go should import plugin/external/sdk")
	}
	if !strings.Contains(provSrc, "sdk.PluginManifest") {
		t.Error("provider.go should use sdk.PluginManifest")
	}
	if !strings.Contains(provSrc, "sdk.StepInstance") {
		t.Error("provider.go should return sdk.StepInstance")
	}
	if !strings.Contains(provSrc, "sdk.ErrTypedContractNotHandled") {
		t.Error("provider.go should let unknown typed step types fall back to legacy providers")
	}

	// Verify steps.go uses external SDK types.
	stepsData, err := os.ReadFile(filepath.Join(outputDir, "internal/steps.go"))
	if err != nil {
		t.Fatalf("read steps.go: %v", err)
	}
	stepsSrc := string(stepsData)
	if !strings.Contains(stepsSrc, `"github.com/GoCodeAlone/workflow/plugin/external/sdk"`) {
		t.Error("steps.go should import plugin/external/sdk")
	}
	if !strings.Contains(stepsSrc, "*sdk.TypedStepResult") {
		t.Error("steps.go should return *sdk.TypedStepResult")
	}

	// Verify go.mod has correct module path.
	modData, err := os.ReadFile(filepath.Join(outputDir, "go.mod"))
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	if !strings.Contains(string(modData), "module github.com/TestOrg/workflow-plugin-my-plugin") {
		t.Errorf("go.mod module path unexpected: %s", string(modData))
	}
	if !strings.Contains(string(modData), "go "+workflowMinimumGoVersion) {
		t.Errorf("go.mod go version should match workflow minimum %s, got:\n%s", workflowMinimumGoVersion, string(modData))
	}

	ciData, err := os.ReadFile(filepath.Join(outputDir, ".github/workflows/ci.yml"))
	if err != nil {
		t.Fatalf("read ci.yml: %v", err)
	}
	if !strings.Contains(string(ciData), "go-version: '"+workflowMinimumGoVersion+"'") {
		t.Errorf("ci.yml should use workflow minimum Go %s, got:\n%s", workflowMinimumGoVersion, string(ciData))
	}

	releaseData, err := os.ReadFile(filepath.Join(outputDir, ".github/workflows/release.yml"))
	if err != nil {
		t.Fatalf("read release.yml: %v", err)
	}
	if !strings.Contains(string(releaseData), "go-version: '"+workflowMinimumGoVersion+"'") {
		t.Errorf("release.yml should use workflow minimum Go %s, got:\n%s", workflowMinimumGoVersion, string(releaseData))
	}
}

func TestGenerateGoModWithWorkflowReplace(t *testing.T) {
	got := generateGoMod("example.com/plugin", "/workspace/workflow", false)
	if !strings.Contains(got, "github.com/GoCodeAlone/workflow "+workflowStrictContractsVersion) {
		t.Fatalf("go.mod should require local-development workflow version, got:\n%s", got)
	}
	if !strings.Contains(got, "go "+workflowMinimumGoVersion) {
		t.Fatalf("strict go.mod should use workflow minimum Go version, got:\n%s", got)
	}
	if !strings.Contains(got, "replace github.com/GoCodeAlone/workflow => /workspace/workflow") {
		t.Fatalf("go.mod should include workflow replace, got:\n%s", got)
	}
}

func TestGenerateGoModLegacyWithoutWorkflowReplace(t *testing.T) {
	got := generateGoMod("example.com/plugin", "", true)
	if !strings.Contains(got, "github.com/GoCodeAlone/workflow "+workflowReleasedVersion) {
		t.Fatalf("legacy go.mod should require released workflow version, got:\n%s", got)
	}
	if !strings.Contains(got, "go "+defaultPluginGoVersion) {
		t.Fatalf("legacy go.mod should keep default plugin Go version, got:\n%s", got)
	}
}

func TestDiscoverWorkflowModuleRootWalksParents(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module github.com/GoCodeAlone/workflow\n"), 0600); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	nested := filepath.Join(root, "cmd", "wfctl")
	if err := os.MkdirAll(nested, 0750); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}
	if got := DiscoverWorkflowModuleRoot(nested); got != root {
		t.Fatalf("discoverWorkflowModuleRoot = %q, want %q", got, root)
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
