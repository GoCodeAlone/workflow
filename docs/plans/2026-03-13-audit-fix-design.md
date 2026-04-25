---
status: implemented
area: core
owner: workflow
implementation_refs:
  - repo: workflow
    commit: 21c20fd
  - repo: workflow
    commit: 1ca0515
  - repo: workflow
    commit: 431d35a
  - repo: workflow
    commit: e05c2c0
external_refs: []
verification:
  last_checked: 2026-04-25
  commands:
    - "rg -n \"knownStepTypeDescriptions|PipelineStep|StepFactory|GetStepSchemaRegistry|drift\" mcp schema interfaces infra"
    - "git log --oneline --all -- mcp/tools.go mcp/wfctl_tools.go interfaces/pipeline.go schema/step_schema_builtins.go schema/step_schema_drift_test.go"
  result: pass
supersedes: []
superseded_by: []
---

# Workflow Audit Fix Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Fix all issues found in the workflow repo audit: eliminate hardcoded MCP descriptions (anti-staleness), extract step interfaces, wire infra provisioner into engine, clean dead code, add missing step schemas, add drift-detection tests, and add unit tests for untested pipeline steps and plugins.

**Architecture:** Replace parallel hardcoded sources of truth with single-source registries + drift-detection tests that fail CI when things diverge. Move `PipelineStep`/`StepFactory` to `interfaces/` so the engine depends on abstractions. Wire `infra.Provisioner` into the engine lifecycle via a provider interface and config hook.

**Tech Stack:** Go 1.26, stdlib testing, no new dependencies

---

## Work Stream A: Anti-Staleness (MCP + Schema Registry)

### Task A1: Delete `knownStepTypeDescriptions()` and replace callers

**Files:**
- Modify: `mcp/tools.go:414-866` (delete structs + function)
- Modify: `mcp/wfctl_tools.go:1313-1340` (mcpCheckCompatibility)
- Modify: `mcp/wfctl_tools.go:1393-1420` (mcpValidateWorkflowConfig)
- Test: `mcp/tools_test.go`

**Step 1: Write a test that `knownStepTypeDescriptions` is no longer called**

In `mcp/tools_test.go`, add a test that verifies `mcpCheckCompatibility` and `mcpValidateWorkflowConfig` only use dynamic sources:

```go
func TestNoHardcodedStepDescriptions(t *testing.T) {
	// Verify the function no longer exists by checking that
	// schema.GetStepSchemaRegistry().Types() covers all step types
	// used in compatibility and validation checks.
	reg := schema.GetStepSchemaRegistry()
	types := reg.Types()
	if len(types) == 0 {
		t.Fatal("step schema registry is empty — builtins not registered")
	}
	// Verify schema registry has entries for core step types
	for _, st := range []string{"step.set", "step.conditional", "step.db_query", "step.http_call"} {
		if reg.Get(st) == nil {
			t.Errorf("schema registry missing core step type %q", st)
		}
	}
}
```

**Step 2: Run test to verify it passes (the registry should already have these)**

Run: `cd /Users/jon/workspace/workflow && go test ./mcp/ -run TestNoHardcodedStepDescriptions -v`

**Step 3: Replace callers in `wfctl_tools.go`**

In `mcpCheckCompatibility` (around L1323), replace:
```go
// OLD:
knownSteps := make(map[string]bool)
for t := range knownStepTypeDescriptions() {
    knownSteps[t] = true
}
// Merge with schema-registered step types
for _, t := range schema.KnownModuleTypes() {
    if strings.HasPrefix(t, "step.") {
        knownSteps[t] = true
    }
}
```
with:
```go
// NEW: single source of truth — schema registry + KnownModuleTypes
knownSteps := make(map[string]bool)
for _, t := range schema.GetStepSchemaRegistry().Types() {
    knownSteps[t] = true
}
for _, t := range schema.KnownModuleTypes() {
    if strings.HasPrefix(t, "step.") {
        knownSteps[t] = true
    }
}
```

In `mcpValidateWorkflowConfig` (around L1403), replace:
```go
// OLD:
knownSteps := knownStepTypeDescriptions()
knownStepSet := make(map[string]bool)
for t := range knownSteps {
    knownStepSet[t] = true
}
for _, t := range schema.KnownModuleTypes() {
    if strings.HasPrefix(t, "step.") {
        knownStepSet[t] = true
    }
}
```
with:
```go
// NEW: single source of truth
knownStepSet := make(map[string]bool)
for _, t := range schema.GetStepSchemaRegistry().Types() {
    knownStepSet[t] = true
}
for _, t := range schema.KnownModuleTypes() {
    if strings.HasPrefix(t, "step.") {
        knownStepSet[t] = true
    }
}
```

Check if `knownSteps` (the full map) is used later in `mcpValidateWorkflowConfig` for richer config key hints. If so, replace those lookups with `schema.GetStepSchemaRegistry().Get(stepType)` which returns a `*StepSchema` with `ConfigFields`.

**Step 4: Delete the hardcoded function and structs from `mcp/tools.go`**

Delete lines 414-866:
- `stepTypeInfoFull` struct (L415-422)
- `stepConfigKeyDef` struct (L424-429)
- `knownStepTypeDescriptions()` function (L431-866)

**Step 5: Fix any test references**

Search `mcp/tools_test.go` for references to `knownStepTypeDescriptions`, `stepTypeInfoFull`, or `stepConfigKeyDef` and remove/replace them.

**Step 6: Run tests**

Run: `cd /Users/jon/workspace/workflow && go test ./mcp/ -v -count=1`
Expected: PASS — no compilation errors, all existing tests pass

**Step 7: Commit**

```bash
git add mcp/tools.go mcp/wfctl_tools.go mcp/tools_test.go
git commit -m "refactor(mcp): replace knownStepTypeDescriptions with schema registry lookups

Remove hardcoded step type descriptions that drifted from the authoritative
schema registry. Both mcpCheckCompatibility and mcpValidateWorkflowConfig
now use schema.GetStepSchemaRegistry() as the single source of truth."
```

---

### Task A2: Create `TemplateFuncRegistry` and replace `templateFunctionDescriptions()`

**Files:**
- Modify: `module/pipeline_template.go:455-842` (add registry alongside funcMap)
- Create: `module/template_func_registry.go` (registry type + exported accessor)
- Modify: `mcp/tools.go:868-1054` (delete hardcoded descriptions, import module registry)
- Test: `module/template_func_registry_test.go`

**Step 1: Create the template function registry type**

Create `module/template_func_registry.go`:

```go
package module

// TemplateFuncDef describes a template function for documentation/MCP.
type TemplateFuncDef struct {
	Name        string `json:"name"`
	Signature   string `json:"signature"`
	Description string `json:"description"`
	Example     string `json:"example"`
}

var templateFuncDefs []TemplateFuncDef

func init() {
	templateFuncDefs = buildTemplateFuncDefs()
}

// TemplateFuncDescriptions returns metadata for all registered template functions.
// This is the single source of truth — MCP tools call this instead of maintaining
// a parallel hardcoded list.
func TemplateFuncDescriptions() []TemplateFuncDef {
	return templateFuncDefs
}

func buildTemplateFuncDefs() []TemplateFuncDef {
	return []TemplateFuncDef{
		{Name: "uuid", Signature: "uuid", Description: "Generates a new UUID v4 string", Example: `{{ uuid }}`},
		{Name: "uuidv4", Signature: "uuidv4", Description: "Alias for uuid — generates a UUID v4 string", Example: `{{ uuidv4 }}`},
		{Name: "now", Signature: "now [layout]", Description: "Returns the current UTC time. Optional layout: RFC3339 (default), unix, date, time, or Go format string", Example: `{{ now "RFC3339" }}`},
		{Name: "lower", Signature: "lower str", Description: "Converts string to lowercase", Example: `{{ lower "HELLO" }}`},
		{Name: "default", Signature: "default defaultVal value", Description: "Returns value if non-nil and non-empty, otherwise defaultVal", Example: `{{ default "N/A" .name }}`},
		{Name: "trimPrefix", Signature: "trimPrefix prefix str", Description: "Removes the prefix from string", Example: `{{ trimPrefix "api/" .path }}`},
		{Name: "trimSuffix", Signature: "trimSuffix suffix str", Description: "Removes the suffix from string", Example: `{{ trimSuffix ".json" .file }}`},
		{Name: "json", Signature: "json value", Description: "Marshals value to JSON string", Example: `{{ json .data }}`},
		{Name: "config", Signature: "config key", Description: "Looks up a value from the config provider registry", Example: `{{ config "db_path" }}`},
		{Name: "upper", Signature: "upper str", Description: "Converts string to uppercase", Example: `{{ upper "hello" }}`},
		{Name: "title", Signature: "title str", Description: "Capitalizes first letter of each word", Example: `{{ title "hello world" }}`},
		{Name: "replace", Signature: "replace old new str", Description: "Replaces all occurrences of old with new in str", Example: `{{ replace "-" "_" .name }}`},
		{Name: "contains", Signature: "contains substr str", Description: "Returns true if str contains substr", Example: `{{ if contains "error" .msg }}...{{ end }}`},
		{Name: "hasPrefix", Signature: "hasPrefix prefix str", Description: "Returns true if str starts with prefix", Example: `{{ if hasPrefix "http" .url }}...{{ end }}`},
		{Name: "hasSuffix", Signature: "hasSuffix suffix str", Description: "Returns true if str ends with suffix", Example: `{{ if hasSuffix ".json" .file }}...{{ end }}`},
		{Name: "split", Signature: "split sep str", Description: "Splits string by separator into array", Example: `{{ split "," .tags }}`},
		{Name: "join", Signature: "join sep array", Description: "Joins array elements with separator", Example: `{{ join ", " .items }}`},
		{Name: "trimSpace", Signature: "trimSpace str", Description: "Removes leading and trailing whitespace", Example: `{{ trimSpace .input }}`},
		{Name: "urlEncode", Signature: "urlEncode str", Description: "URL-encodes a string", Example: `{{ urlEncode .query }}`},
		{Name: "add", Signature: "add a b", Description: "Adds two numbers (int64 or float64)", Example: `{{ add 1 2 }}`},
		{Name: "sub", Signature: "sub a b", Description: "Subtracts b from a", Example: `{{ sub 10 3 }}`},
		{Name: "mul", Signature: "mul a b", Description: "Multiplies two numbers", Example: `{{ mul 5 3 }}`},
		{Name: "div", Signature: "div a b", Description: "Divides a by b (returns float64, zero-safe)", Example: `{{ div 10 3 }}`},
		{Name: "toInt", Signature: "toInt value", Description: "Converts value to int64", Example: `{{ toInt "42" }}`},
		{Name: "toFloat", Signature: "toFloat value", Description: "Converts value to float64", Example: `{{ toFloat "3.14" }}`},
		{Name: "toString", Signature: "toString value", Description: "Converts any value to its string representation", Example: `{{ toString 42 }}`},
		{Name: "length", Signature: "length collection", Description: "Returns the length of a slice, map, or string", Example: `{{ length .items }}`},
		{Name: "coalesce", Signature: "coalesce values...", Description: "Returns the first non-nil, non-empty value", Example: `{{ coalesce .preferred .fallback "default" }}`},
		{Name: "sum", Signature: "sum collection [field]", Description: "Sums numeric values in a collection. Optional field name for slice of maps", Example: `{{ sum .prices "amount" }}`},
		{Name: "pluck", Signature: "pluck field collection", Description: "Extracts a field value from each map in a slice", Example: `{{ pluck "name" .users }}`},
		{Name: "flatten", Signature: "flatten collection", Description: "Flattens one level of nested slices", Example: `{{ flatten .nested }}`},
		{Name: "unique", Signature: "unique collection", Description: "Deduplicates values preserving insertion order", Example: `{{ unique .tags }}`},
		{Name: "groupBy", Signature: "groupBy field collection", Description: "Groups slice elements by a key field into a map", Example: `{{ groupBy "status" .orders }}`},
		{Name: "sortBy", Signature: "sortBy field collection", Description: "Stable sorts slice of maps by a key field", Example: `{{ sortBy "name" .users }}`},
		{Name: "first", Signature: "first collection", Description: "Returns the first element of a slice", Example: `{{ first .items }}`},
		{Name: "last", Signature: "last collection", Description: "Returns the last element of a slice", Example: `{{ last .items }}`},
		{Name: "min", Signature: "min collection [field]", Description: "Returns the minimum numeric value. Optional field for slice of maps", Example: `{{ min .scores }}`},
		{Name: "max", Signature: "max collection [field]", Description: "Returns the maximum numeric value. Optional field for slice of maps", Example: `{{ max .scores }}`},
		// Context-bound functions (added by funcMapWithContext, not in templateFuncMap)
		{Name: "step", Signature: "step name [key...]", Description: "Accesses a previous step's output by name and optional nested keys", Example: `{{ step "fetch_user" "row" "email" }}`},
		{Name: "trigger", Signature: "trigger [key...]", Description: "Accesses trigger data by optional nested keys", Example: `{{ trigger "body" "user_id" }}`},
	}
}
```

**Step 2: Write drift-detection test**

Create `module/template_func_registry_test.go`:

```go
package module

import (
	"testing"
)

func TestTemplateFuncDescriptionsCoversFuncMap(t *testing.T) {
	fm := templateFuncMap()
	defs := TemplateFuncDescriptions()

	// Build set of documented function names
	documented := make(map[string]bool, len(defs))
	for _, d := range defs {
		documented[d.Name] = true
	}

	// Every function in templateFuncMap must have a description
	for name := range fm {
		if !documented[name] {
			t.Errorf("template function %q is in templateFuncMap() but has no TemplateFuncDef — add it to buildTemplateFuncDefs()", name)
		}
	}

	// Context-bound functions (step, trigger) are documented but not in the base funcMap
	// They're added by funcMapWithContext — skip checking them in the other direction
	baseFuncs := make(map[string]bool, len(fm))
	for name := range fm {
		baseFuncs[name] = true
	}
	contextFuncs := map[string]bool{"step": true, "trigger": true}

	for _, d := range defs {
		if !baseFuncs[d.Name] && !contextFuncs[d.Name] {
			t.Errorf("TemplateFuncDef %q has no matching function in templateFuncMap() or funcMapWithContext()", d.Name)
		}
	}
}

func TestTemplateFuncDescriptionsComplete(t *testing.T) {
	defs := TemplateFuncDescriptions()
	for _, d := range defs {
		if d.Name == "" {
			t.Error("TemplateFuncDef has empty Name")
		}
		if d.Signature == "" {
			t.Errorf("TemplateFuncDef %q has empty Signature", d.Name)
		}
		if d.Description == "" {
			t.Errorf("TemplateFuncDef %q has empty Description", d.Name)
		}
		if d.Example == "" {
			t.Errorf("TemplateFuncDef %q has empty Example", d.Name)
		}
	}
}
```

**Step 3: Run tests**

Run: `cd /Users/jon/workspace/workflow && go test ./module/ -run TestTemplateFuncDescriptions -v`
Expected: PASS

**Step 4: Replace `templateFunctionDescriptions()` in MCP tools.go**

In `mcp/tools.go`, delete the `TemplateFunctionDef` struct (L868-874) and `templateFunctionDescriptions()` function (L877-1054).

Replace `handleGetTemplateFunctions` (L221-227):
```go
func (s *Server) handleGetTemplateFunctions(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	funcs := module.TemplateFuncDescriptions()
	return marshalToolResult(map[string]any{
		"functions": funcs,
		"count":     len(funcs),
	})
}
```

Add `"github.com/GoCodeAlone/workflow/module"` to imports if not already present.

**Step 5: Run full MCP tests**

Run: `cd /Users/jon/workspace/workflow && go test ./mcp/ -v -count=1`
Expected: PASS

**Step 6: Commit**

```bash
git add module/template_func_registry.go module/template_func_registry_test.go mcp/tools.go
git commit -m "refactor(mcp): replace hardcoded template func descriptions with module registry

Create TemplateFuncDef registry in module/ alongside templateFuncMap() so
descriptions live next to implementations. Add drift-detection test that
fails when a template function exists without a description entry.
Adds 12 previously undocumented functions: sum, pluck, flatten, unique,
groupBy, sortBy, first, last, min, max, step, trigger."
```

---

### Task A3: Fix schema defaults and add missing step schemas

**Files:**
- Modify: `schema/step_schema_builtins.go` (fix db_query default, conditional required, add missing schemas)
- Modify: `schema/schema.go:145-272` (add missing types to coreModuleTypes)
- Test: `schema/step_schema_drift_test.go` (new drift detection test)

**Step 1: Write drift-detection tests**

Create `schema/step_schema_drift_test.go`:

```go
package schema

import (
	"strings"
	"testing"
)

// TestCoreStepTypesHaveSchemas verifies every step.* entry in coreModuleTypes
// has a corresponding StepSchemaRegistry entry. Fails when a step type is added
// to coreModuleTypes without a schema.
func TestCoreStepTypesHaveSchemas(t *testing.T) {
	reg := GetStepSchemaRegistry()
	for _, mt := range coreModuleTypes {
		if !strings.HasPrefix(mt, "step.") {
			continue
		}
		if reg.Get(mt) == nil {
			t.Errorf("step type %q is in coreModuleTypes but has no StepSchemaRegistry entry — add it to registerBuiltins()", mt)
		}
	}
}

// TestSchemaRegistryTypesInCoreModuleTypes verifies every step type registered
// in the StepSchemaRegistry is also listed in coreModuleTypes. Fails when a
// schema is added to registerBuiltins() without updating coreModuleTypes.
func TestSchemaRegistryTypesInCoreModuleTypes(t *testing.T) {
	coreSet := make(map[string]bool, len(coreModuleTypes))
	for _, mt := range coreModuleTypes {
		coreSet[mt] = true
	}
	for _, st := range GetStepSchemaRegistry().Types() {
		if !coreSet[st] {
			t.Errorf("step type %q is in StepSchemaRegistry but not in coreModuleTypes — add it to schema.go", st)
		}
	}
}
```

**Step 2: Run tests to see current failures**

Run: `cd /Users/jon/workspace/workflow && go test ./schema/ -run TestCoreStepTypesHaveSchemas -v`
Expected: FAIL — lists all step types missing schemas

Run: `cd /Users/jon/workspace/workflow && go test ./schema/ -run TestSchemaRegistryTypesInCoreModuleTypes -v`
Expected: FAIL — lists all schema types missing from coreModuleTypes

**Step 3: Fix `step.db_query` default**

In `schema/step_schema_builtins.go`, find the `step.db_query` registration (around L123). Change the `mode` field's `DefaultValue` from `"single"` to `"list"`:

```go
{Key: "mode", Label: "Mode", Type: FieldTypeSelect, Description: "Query mode: list returns rows array, single returns first row", DefaultValue: "list", Options: []string{"list", "single"}},
```

**Step 4: Fix `step.conditional` routes required**

In `schema/step_schema_builtins.go`, find the `step.conditional` registration (around L56). Set `Required: true` on the `routes` field:

```go
{Key: "routes", Label: "Routes", Type: FieldTypeMap, Description: "Map of field values to next step names", Required: true, MapValueType: "string"},
```

**Step 5: Add missing step types to `coreModuleTypes`**

In `schema/schema.go`, add the following to the `coreModuleTypes` slice (in alphabetical order within the step.* section):

```go
// Add these missing step types (registered by schema or factories but not in coreModuleTypes):
"step.auth_required",
"step.auth_validate",
"step.authz_check",
"step.cli_invoke",
"step.cli_print",
"step.event_decrypt",
"step.field_reencrypt",
"step.graphql",
"step.json_parse",
"step.m2m_token",
"step.nosql_delete",
"step.nosql_get",
"step.nosql_put",
"step.nosql_query",
"step.oidc_auth_url",
"step.oidc_callback",
"step.raw_response",
"step.sandbox_exec",
"step.secret_fetch",
"step.statemachine_get",
"step.statemachine_transition",
"step.token_revoke",
```

Also add all factory-registered step types from plugins that are missing. These include CI/CD, platform, storage, marketplace, policy, gitlab, and actor step types. There are ~80 of them — add them all alphabetically. The exact list is in the research notes (Task A3 research: "Factories registered but NOT in coreModuleTypes").

**Step 6: Add missing step schemas to `registerBuiltins()`**

For every step type that is now in `coreModuleTypes` but has no schema entry, add a minimal schema to `step_schema_builtins.go`. At minimum each schema needs: Type, Plugin, Description, and at least a ConfigFields with the primary config keys the step reads.

For the pipelinesteps plugin steps that are missing schemas, look at the corresponding `pipeline_step_*.go` factory function to see what config keys it reads, then add the schema. Here are the key ones:

```go
// step.raw_response — from pipeline_step_raw_response.go
r.Register(&StepSchema{
    Type: "step.raw_response", Plugin: "pipelinesteps",
    Description: "Sends a raw HTTP response with configurable status, headers, and body",
    ConfigFields: []ConfigFieldDef{
        {Key: "status", Label: "Status Code", Type: FieldTypeNumber, Description: "HTTP status code", DefaultValue: 200},
        {Key: "headers", Label: "Headers", Type: FieldTypeMap, Description: "Response headers"},
        {Key: "body", Label: "Body", Type: FieldTypeString, Description: "Response body template"},
    },
})

// step.json_parse — from pipeline_step_json_parse.go
r.Register(&StepSchema{
    Type: "step.json_parse", Plugin: "pipelinesteps",
    Description: "Parses a JSON string into a structured map",
    ConfigFields: []ConfigFieldDef{
        {Key: "input", Label: "Input", Type: FieldTypeString, Description: "Template expression that resolves to a JSON string", Required: true},
    },
    Outputs: []StepOutputDef{
        {Key: "parsed", Type: "map", Description: "Parsed JSON object"},
    },
})

// step.auth_validate — from pipeline_step_auth_validate.go
r.Register(&StepSchema{
    Type: "step.auth_validate", Plugin: "pipelinesteps",
    Description: "Validates JWT or API key authentication from the request",
    ConfigFields: []ConfigFieldDef{
        {Key: "type", Label: "Auth Type", Type: FieldTypeSelect, Description: "Authentication type to validate", Options: []string{"jwt", "api_key"}, DefaultValue: "jwt"},
        {Key: "secret", Label: "Secret", Type: FieldTypeString, Description: "JWT signing secret or API key store name", Sensitive: true},
        {Key: "header", Label: "Header", Type: FieldTypeString, Description: "Header name to read token from", DefaultValue: "Authorization"},
    },
    Outputs: []StepOutputDef{
        {Key: "valid", Type: "bool", Description: "Whether authentication succeeded"},
        {Key: "claims", Type: "map", Description: "Decoded JWT claims or API key metadata"},
    },
})

// step.token_revoke — from pipeline_step_token_revoke.go
r.Register(&StepSchema{
    Type: "step.token_revoke", Plugin: "pipelinesteps",
    Description: "Adds a JWT token to the revocation blacklist",
    ConfigFields: []ConfigFieldDef{
        {Key: "token", Label: "Token", Type: FieldTypeString, Description: "The JWT token to revoke", Required: true},
    },
})

// step.field_reencrypt
r.Register(&StepSchema{
    Type: "step.field_reencrypt", Plugin: "pipelinesteps",
    Description: "Re-encrypts a field value with a new key",
    ConfigFields: []ConfigFieldDef{
        {Key: "field", Label: "Field", Type: FieldTypeString, Description: "Dot-path to the field to re-encrypt", Required: true},
        {Key: "old_key", Label: "Old Key", Type: FieldTypeString, Description: "Current encryption key name", Required: true, Sensitive: true},
        {Key: "new_key", Label: "New Key", Type: FieldTypeString, Description: "New encryption key name", Required: true, Sensitive: true},
    },
})

// step.sandbox_exec
r.Register(&StepSchema{
    Type: "step.sandbox_exec", Plugin: "pipelinesteps",
    Description: "Executes code in a sandboxed environment",
    ConfigFields: []ConfigFieldDef{
        {Key: "runtime", Label: "Runtime", Type: FieldTypeSelect, Description: "Sandbox runtime", Options: []string{"wasm", "docker", "process"}},
        {Key: "code", Label: "Code", Type: FieldTypeString, Description: "Code to execute"},
        {Key: "timeout", Label: "Timeout", Type: FieldTypeDuration, Description: "Execution timeout", DefaultValue: "30s"},
    },
    Outputs: []StepOutputDef{
        {Key: "output", Type: "string", Description: "Execution output"},
        {Key: "exit_code", Type: "number", Description: "Exit code"},
    },
})

// step.cli_print
r.Register(&StepSchema{
    Type: "step.cli_print", Plugin: "pipelinesteps",
    Description: "Prints formatted output to the CLI",
    ConfigFields: []ConfigFieldDef{
        {Key: "message", Label: "Message", Type: FieldTypeString, Description: "Message template to print", Required: true},
        {Key: "format", Label: "Format", Type: FieldTypeSelect, Description: "Output format", Options: []string{"text", "json", "table"}, DefaultValue: "text"},
    },
})

// step.cli_invoke
r.Register(&StepSchema{
    Type: "step.cli_invoke", Plugin: "pipelinesteps",
    Description: "Invokes a registered CLI command",
    ConfigFields: []ConfigFieldDef{
        {Key: "command", Label: "Command", Type: FieldTypeString, Description: "CLI command name to invoke", Required: true},
        {Key: "args", Label: "Arguments", Type: FieldTypeArray, Description: "Command arguments", ArrayItemType: "string"},
    },
    Outputs: []StepOutputDef{
        {Key: "output", Type: "string", Description: "Command output"},
        {Key: "exit_code", Type: "number", Description: "Exit code"},
    },
})

// step.graphql
r.Register(&StepSchema{
    Type: "step.graphql", Plugin: "pipelinesteps",
    Description: "Executes a GraphQL query or mutation against an endpoint",
    ConfigFields: []ConfigFieldDef{
        {Key: "url", Label: "URL", Type: FieldTypeString, Description: "GraphQL endpoint URL", Required: true},
        {Key: "query", Label: "Query", Type: FieldTypeString, Description: "GraphQL query or mutation string", Required: true},
        {Key: "variables", Label: "Variables", Type: FieldTypeMap, Description: "Query variables"},
        {Key: "headers", Label: "Headers", Type: FieldTypeMap, Description: "HTTP headers"},
    },
    Outputs: []StepOutputDef{
        {Key: "data", Type: "map", Description: "GraphQL response data"},
        {Key: "errors", Type: "array", Description: "GraphQL errors if any"},
    },
})

// step.event_decrypt
r.Register(&StepSchema{
    Type: "step.event_decrypt", Plugin: "pipelinesteps",
    Description: "Decrypts an encrypted event payload",
    ConfigFields: []ConfigFieldDef{
        {Key: "key", Label: "Key", Type: FieldTypeString, Description: "Encryption key name", Required: true, Sensitive: true},
        {Key: "field", Label: "Field", Type: FieldTypeString, Description: "Dot-path to encrypted field", Required: true},
    },
    Outputs: []StepOutputDef{
        {Key: "decrypted", Type: "map", Description: "Decrypted data"},
    },
})

// step.secret_fetch
r.Register(&StepSchema{
    Type: "step.secret_fetch", Plugin: "pipelinesteps",
    Description: "Fetches a secret from the configured secrets provider",
    ConfigFields: []ConfigFieldDef{
        {Key: "name", Label: "Secret Name", Type: FieldTypeString, Description: "Name/path of the secret to fetch", Required: true},
        {Key: "provider", Label: "Provider", Type: FieldTypeSelect, Description: "Secrets provider", Options: []string{"vault", "aws", "env"}},
    },
    Outputs: []StepOutputDef{
        {Key: "value", Type: "string", Description: "Secret value"},
    },
})
```

For plugin step types (cicd, platform, datastores, etc.), add schemas via each plugin's `StepSchemas()` method rather than in `registerBuiltins()`. The agent implementing this should read each plugin's step factory files to determine the correct config fields. If the plugin already returns schemas from `StepSchemas()`, verify they're loaded correctly.

**Step 7: Run drift detection tests**

Run: `cd /Users/jon/workspace/workflow && go test ./schema/ -run TestCoreStepTypes -v`
Expected: PASS (or reduced failures — some plugin steps may be registered dynamically)

**Step 8: Commit**

```bash
git add schema/step_schema_builtins.go schema/schema.go schema/step_schema_drift_test.go
git commit -m "fix(schema): fix defaults, add missing schemas, add drift-detection tests

Fix step.db_query default from 'single' to 'list' to match implementation.
Mark step.conditional routes as required. Add 11 missing step schemas for
pipelinesteps plugin. Add 80+ missing step types to coreModuleTypes.
Add drift-detection tests that fail CI when schema/coreModuleTypes diverge."
```

---

### Task A4: Fix README and documentation

**Files:**
- Modify: `README.md`
- Modify: `DOCUMENTATION.md` (if it exists)
- Modify: `docs/mcp.md` (if it exists)

**Step 1: Fix modular version references**

Search README.md for `CrisisTextLine` and replace with `GoCodeAlone`. Search for `v1.11.11` and replace with `v1.12.0` (check go.mod for the exact version).

**Step 2: Fix command syntax**

Search for `api-extract` and replace with `api extract` (space, not hyphen).

**Step 3: Fix module/step counts**

Replace any hardcoded count (like "48 module types") with the actual count from the code. Use language like "90+ module types" rather than exact numbers to reduce staleness.

**Step 4: Update `docs/mcp.md` tool list**

If this file exists, update it to list all registered MCP tools. Check `mcp/server.go` for the full list of registered tools and ensure the doc matches.

**Step 5: Commit**

```bash
git add README.md DOCUMENTATION.md docs/mcp.md
git commit -m "docs: fix stale version refs, command syntax, module counts"
```

---

## Work Stream B: Interface Extraction (Phase 2)

### Task B1: Define step interfaces in `interfaces/`

**Files:**
- Modify: `interfaces/pipeline.go`
- Test: `interfaces/pipeline_test.go` (compile check)

**Step 1: Add PipelineStep, StepFactory, and StepRegistrar to interfaces/pipeline.go**

Add these types to `interfaces/pipeline.go` (after the existing `StepRegistryProvider`):

```go
import (
	"context"
)

// PipelineStep is a single composable unit of work in a pipeline.
type PipelineStep interface {
	Name() string
	Execute(ctx context.Context, stepOutputs, current, metadata map[string]any) (output map[string]any, nextStep string, stop bool, err error)
}
```

Wait — the current `module.PipelineStep.Execute` takes `*PipelineContext` which is a module-level type. Moving to interfaces requires either:
(a) Moving `PipelineContext` and `StepResult` to interfaces too, or
(b) Using a flattened signature in the interface

Option (a) is cleaner. Add to `interfaces/pipeline.go`:

```go
// PipelineContext carries the execution state through a pipeline.
type PipelineContext struct {
	TriggerData map[string]any
	StepOutputs map[string]map[string]any
	Current     map[string]any
	Metadata    map[string]any
}

// StepResult is the outcome of a pipeline step execution.
type StepResult struct {
	Output   map[string]any
	NextStep string
	Stop     bool
}

// PipelineStep is a single composable unit of work in a pipeline.
type PipelineStep interface {
	Name() string
	Execute(ctx context.Context, pc *PipelineContext) (*StepResult, error)
}

// StepFactory creates a PipelineStep from a name, config, and application.
type StepFactory func(name string, config map[string]any, app any) (PipelineStep, error)

// StepRegistrar manages step type registration and creation.
type StepRegistrar interface {
	StepRegistryProvider // embeds Types() []string
	Register(stepType string, factory StepFactory)
	Create(stepType, name string, config map[string]any, app any) (PipelineStep, error)
}
```

**NOTE:** The `app` parameter in the current code is `modular.Application`. Using `any` in the interface avoids coupling `interfaces/` to the `modular` package. The concrete implementation casts internally.

**Step 2: Make `module.PipelineStep` a type alias**

In `module/pipeline_step.go`, change from interface definition to alias:

```go
package module

import "github.com/GoCodeAlone/workflow/interfaces"

// PipelineStep is a single composable unit of work in a pipeline.
// Aliased from interfaces.PipelineStep for backwards compatibility.
type PipelineStep = interfaces.PipelineStep
```

Similarly for `PipelineContext` and `StepResult` in `module/pipeline_context.go` — alias them to the interfaces package types.

**Step 3: Make `module.StepFactory` a type alias**

In `module/pipeline_step_registry.go`:
```go
// StepFactory creates a PipelineStep from config.
type StepFactory = interfaces.StepFactory
```

Wait — the current `StepFactory` signature uses `modular.Application`, not `any`. We need to be careful here. The interface uses `any` but the module type uses `modular.Application`. A type alias won't work directly.

**Revised approach:** Keep `module.StepFactory` as-is (with `modular.Application`). Have `StepRegistry` implement `interfaces.StepRegistrar` by wrapping the factory calls. The `interfaces.StepFactory` uses `any` for the app parameter, and `module.StepRegistry.Create()` casts `any` to `modular.Application` internally.

Actually, the simplest approach that doesn't break any plugin code:
1. Define `interfaces.PipelineStep` = exact same interface
2. Make `module.PipelineStep` = `interfaces.PipelineStep` (type alias)
3. Same for `PipelineContext`, `StepResult`
4. Keep `module.StepFactory` as concrete type (it references `modular.Application`)
5. Define `interfaces.StepRegistrar` with `Create` returning `interfaces.PipelineStep`
6. `engine.stepRegistry` changes to `interfaces.StepRegistrar`

This is the approach. The agent implementing this should:
- Read `module/pipeline_context.go` for the full `PipelineContext` and `StepResult` definitions (may have more fields than shown above)
- Verify all 200+ files that reference `module.PipelineStep` still compile after the alias change
- Run `go build ./...` after every change

**Step 4: Update engine.go**

Change:
```go
// L71: stepRegistry    *module.StepRegistry
stepRegistry    interfaces.StepRegistrar
```

Update `GetStepRegistry()` return type:
```go
func (e *StdEngine) GetStepRegistry() interfaces.StepRegistrar {
```

**Step 5: Run full build**

Run: `cd /Users/jon/workspace/workflow && go build ./...`
Expected: PASS — all packages compile

**Step 6: Run full tests**

Run: `cd /Users/jon/workspace/workflow && go test ./... -count=1`
Expected: PASS

**Step 7: Commit**

```bash
git add interfaces/pipeline.go module/pipeline_step.go module/pipeline_context.go module/pipeline_step_registry.go engine.go
git commit -m "refactor: extract PipelineStep/StepResult/PipelineContext to interfaces package

Move step abstractions to interfaces/ so the engine depends on abstractions
rather than the concrete module package. module.PipelineStep is now a type
alias for interfaces.PipelineStep, maintaining full backwards compatibility.
Closes TODO(phase5)."
```

---

## Work Stream C: Infrastructure Provisioner (Phase 4)

### Task C1: Add ResourceProvisioner provider interface

**Files:**
- Create: `infra/provider.go`
- Modify: `infra/provisioner.go` (extract memory provider, use interface)
- Test: `infra/provider_test.go`

**Step 1: Define provider interface**

Create `infra/provider.go`:

```go
package infra

import "context"

// ResourceProvider provisions and destroys a specific resource type.
// Implementations handle the actual infrastructure operations (memory, SQLite,
// cloud APIs, etc.) while the Provisioner orchestrates the lifecycle.
type ResourceProvider interface {
	// Provision creates or updates a resource. Returns an error if the provider
	// is not configured or the resource config is invalid.
	Provision(ctx context.Context, rc ResourceConfig) error

	// Destroy removes a provisioned resource by name.
	Destroy(ctx context.Context, name string) error

	// Supports returns true if this provider handles the given resource type + provider combo.
	Supports(resourceType, provider string) bool
}
```

**Step 2: Extract MemoryProvider from current Provisioner**

The current `provisionResource()` at L256 does mock provisioning (just stores in the map). Extract this into a `MemoryProvider` struct that implements `ResourceProvider`:

```go
// MemoryProvider is an in-memory resource provider for testing and local development.
type MemoryProvider struct{}

func (m *MemoryProvider) Provision(_ context.Context, _ ResourceConfig) error { return nil }
func (m *MemoryProvider) Destroy(_ context.Context, _ string) error           { return nil }
func (m *MemoryProvider) Supports(_, _ string) bool                           { return true }
```

**Step 3: Update Provisioner to use providers**

Add a `providers []ResourceProvider` field to `Provisioner`. In `provisionResource()`, iterate providers to find one that `Supports()` the resource type. If none found, return a clear error.

**Step 4: Write tests**

```go
func TestProvisionerWithProvider(t *testing.T) {
	p := NewProvisioner(nil)
	p.AddProvider(&MemoryProvider{})
	plan := &ProvisionPlan{
		Create: []ResourceConfig{{Name: "test-db", Type: "database", Provider: "memory"}},
	}
	if err := p.Apply(context.Background(), plan); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}
	status := p.Status()
	if _, ok := status["test-db"]; !ok {
		t.Error("expected test-db in status")
	}
}
```

**Step 5: Run tests**

Run: `cd /Users/jon/workspace/workflow && go test ./infra/ -v`
Expected: PASS

**Step 6: Commit**

```bash
git add infra/provider.go infra/provisioner.go infra/provider_test.go
git commit -m "feat(infra): add ResourceProvider interface and extract MemoryProvider

Introduce provider pattern so the Provisioner delegates to pluggable
backends. Extract existing mock logic into MemoryProvider. This enables
SQLite, Postgres, and cloud providers to be added incrementally."
```

### Task C2: Wire Provisioner into engine config

**Files:**
- Modify: `config/config.go` (add Infrastructure field to WorkflowConfig)
- Modify: `engine.go` (add provisioner field, wire into BuildFromConfig)
- Test: existing engine tests should still pass

**Step 1: Add Infrastructure to WorkflowConfig**

In `config/config.go`, add to the `WorkflowConfig` struct:

```go
Infrastructure *InfrastructureConfig `json:"infrastructure,omitempty" yaml:"infrastructure,omitempty"`
```

Where:
```go
type InfrastructureConfig struct {
	Resources []InfraResourceConfig `json:"resources" yaml:"resources"`
}

type InfraResourceConfig struct {
	Name     string         `json:"name" yaml:"name"`
	Type     string         `json:"type" yaml:"type"`
	Provider string         `json:"provider" yaml:"provider"`
	Config   map[string]any `json:"config,omitempty" yaml:"config,omitempty"`
}
```

**Step 2: Add provisioner to engine**

In `engine.go`, add field:
```go
provisioner *infra.Provisioner
```

In `NewStdEngine()` or `BuildFromConfig()`, if `cfg.Infrastructure != nil`:
```go
p := infra.NewProvisioner(e.logger)
p.AddProvider(&infra.MemoryProvider{})
infraCfg := &infra.InfraConfig{
    Resources: convertInfraResources(cfg.Infrastructure.Resources),
}
plan, err := p.Plan(*infraCfg)
if err != nil {
    return fmt.Errorf("infrastructure plan failed: %w", err)
}
if err := p.Apply(ctx, plan); err != nil {
    return fmt.Errorf("infrastructure provisioning failed: %w", err)
}
e.provisioner = p
```

**Step 3: Run tests**

Run: `cd /Users/jon/workspace/workflow && go test ./... -count=1`
Expected: PASS — existing configs without `infrastructure:` key should be unaffected

**Step 4: Commit**

```bash
git add config/config.go engine.go
git commit -m "feat: wire infra.Provisioner into engine lifecycle

Add optional 'infrastructure' config block to WorkflowConfig. When present,
the engine creates a Provisioner, computes a plan, and applies it during
BuildFromConfig. Uses MemoryProvider by default; real providers can be
registered via plugins."
```

---

## Work Stream D: Dead Code Cleanup (Phase 5)

### Task D1: Delete dead code

**Files:**
- Delete: `module/errors.go`
- Verify: no references exist

**Step 1: Verify no references**

Run: `grep -r "ErrNotImplemented" /Users/jon/workspace/workflow --include="*.go" | grep -v errors.go | grep -v "_test.go" | grep -v "docs/" | grep -v "plans/"`
Expected: no output

**Step 2: Delete the file**

```bash
rm module/errors.go
```

**Step 3: Run build**

Run: `cd /Users/jon/workspace/workflow && go build ./...`
Expected: PASS

**Step 4: Commit**

```bash
git add -u module/errors.go
git commit -m "chore: delete dead ErrNotImplemented variable

Was declared but never referenced anywhere in the codebase. The scan steps
that originally returned it have been rewritten to use service provider
interfaces instead."
```

---

## Work Stream E: Unit Tests for Pipeline Steps (Phase 6)

Each task adds tests for a group of related steps. Tests follow the pattern in `pipeline_step_http_call_test.go`: create a `PipelineContext`, create the step via its factory, call `Execute`, and assert outputs.

### Task E1: Database step tests (CRITICAL)

**Files:**
- Create: `module/pipeline_step_db_query_test.go`
- Create: `module/pipeline_step_db_exec_test.go`
- Create: `module/pipeline_step_db_query_cached_test.go`

These steps require a `DBProvider` service in the app's `SvcRegistry()`. Create a test helper:

```go
// testDBProvider wraps an in-memory SQLite DB for testing.
type testDBProvider struct {
	db *sql.DB
}

func newTestDBProvider(t *testing.T) *testDBProvider {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return &testDBProvider{db: db}
}

func (p *testDBProvider) DB() *sql.DB { return p.db }
```

Use a mock `modular.Application` that returns this provider from `SvcRegistry()`.

**Test cases for step.db_query:**
- Factory creation with valid config
- Factory rejects missing `database` key
- Execute with `mode: list` — returns `rows` array and `count`
- Execute with `mode: single` — returns `row` map and `found` bool
- Execute with parameterized query (`$1` placeholders)
- Execute with empty result set
- Execute with missing database service — returns error

**Test cases for step.db_exec:**
- Factory creation with valid config
- Execute INSERT — returns `rows_affected`
- Execute with parameterized query
- Execute with SQL error — returns error

**Test cases for step.db_query_cached:**
- Factory creation with valid config
- First call — cache miss, queries DB
- Second call with same params — cache hit
- Different params — separate cache entries

**Step: Run tests**

Run: `cd /Users/jon/workspace/workflow && go test ./module/ -run TestDBQuery -v`
Expected: PASS

**Commit after each test file is green.**

### Task E2: Core logic step tests

**Files:**
- Create: `module/pipeline_step_json_parse_test.go`
- Create: `module/pipeline_step_json_response_test.go`
- Create: `module/pipeline_step_raw_response_test.go`
- Create: `module/pipeline_step_validate_test.go`
- Create: `module/pipeline_step_http_proxy_test.go`
- Create: `module/pipeline_step_publish_test.go`
- Create: `module/pipeline_step_jq_test.go`
- Create: `module/pipeline_step_resilience_test.go`

For each step, read the implementation file first to understand:
- What config keys the factory expects
- What the Execute method does
- What services it looks up from the app
- What outputs it produces

Write table-driven tests covering: valid config, invalid config, happy path execution, error cases.

### Task E3: Integration-heavy step tests (mock external deps)

**Files:**
- Create: `module/pipeline_step_graphql_test.go`
- Create: `module/pipeline_step_nosql_test.go` (covers nosql_get, nosql_put, nosql_query)
- Create: `module/pipeline_step_statemachine_test.go` (covers transition + get)
- Create: `module/pipeline_step_feature_flag_test.go`
- Create: `module/pipeline_step_policy_test.go`
- Create: `module/pipeline_step_shell_exec_test.go`
- Create: `module/pipeline_step_sandbox_exec_test.go`

For GraphQL: use `httptest.NewServer` as mock GraphQL endpoint.
For NoSQL: mock the NoSQL service interface.
For state machine: mock the state machine engine service.
For shell_exec: test with simple commands like `echo` and verify output capture.

### Task E4: Cloud/CI step tests

**Files:**
- Create: `module/pipeline_step_docker_build_test.go`
- Create: `module/pipeline_step_docker_push_test.go`
- Create: `module/pipeline_step_docker_run_test.go`
- Create: `module/pipeline_step_deploy_test.go`
- Create: `module/pipeline_step_s3_upload_test.go`
- Create: `module/pipeline_step_artifact_test.go`
- Create: `module/pipeline_step_scan_sast_test.go`
- Create: `module/pipeline_step_scan_deps_test.go`
- Create: `module/pipeline_step_scan_container_test.go`

These steps typically call external services. Test:
- Factory creation with valid/invalid config
- Execute with mock service provider (if the step uses app.SvcRegistry)
- Execute with missing service — verify clear error message

### Task E5: Remaining step tests

**Files:**
- Create: `module/pipeline_step_cli_print_test.go`
- Create: `module/pipeline_step_cli_invoke_test.go`
- Create: `module/pipeline_step_ai_classify_test.go`
- Create: `module/pipeline_step_ai_complete_test.go`
- Create: `module/pipeline_step_ai_extract_test.go`
- Create: `module/pipeline_step_gitlab_test.go`
- Create: `module/pipeline_step_secret_fetch_test.go`
- Create: `module/pipeline_step_field_reencrypt_test.go`
- Create: `module/pipeline_step_event_decrypt_test.go`
- Create: `module/pipeline_step_delegate_test.go`
- Create: `module/pipeline_step_app_test.go`
- Create: `module/pipeline_step_platform_template_test.go`

Same pattern: read impl → write tests for factory + execute.

---

## Work Stream F: Plugin Package Tests (Phase 7)

### Task F1: Plugin tests for all 8 untested plugins

**Files:**
- Create: `plugins/k8s/plugin_test.go`
- Create: `plugins/cloud/plugin_test.go`
- Create: `plugins/gitlab/plugin_test.go`
- Create: `plugins/policy/plugin_test.go`
- Create: `plugins/marketplace/plugin_test.go`
- Create: `plugins/openapi/plugin_test.go`
- Create: `plugins/datastores/plugin_test.go`
- Create: `plugins/platform/plugin_test.go`

Each test file follows the pattern from `plugins/pipelinesteps/plugin_test.go`:

```go
func TestPlugin(t *testing.T) {
	p := New()

	t.Run("manifest validates", func(t *testing.T) {
		m := p.EngineManifest()
		if err := m.Validate(); err != nil {
			t.Fatalf("manifest validation failed: %v", err)
		}
	})

	t.Run("step factories registered", func(t *testing.T) {
		sf := p.StepFactories()
		if len(sf) == 0 {
			t.Fatal("no step factories registered")
		}
		// Verify key step types exist
		for _, st := range []string{"step.expected_type_1", "step.expected_type_2"} {
			if _, ok := sf[st]; !ok {
				t.Errorf("missing step factory for %q", st)
			}
		}
	})

	t.Run("module factories registered", func(t *testing.T) {
		mf := p.ModuleFactories()
		// Some plugins may not have module factories — adjust per plugin
		_ = mf
	})
}
```

Read each plugin's `plugin.go` to find the correct step/module types to assert.

---

## Work Stream G: Drift-Detection CI Test

### Task G1: Add comprehensive consistency test

**Files:**
- Create: `consistency_test.go` (package workflow, root level)

This is the capstone test that ties everything together:

```go
package workflow

import (
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/module"
	"github.com/GoCodeAlone/workflow/schema"
)

// TestRegistryConsistency ensures all registration sources agree.
// This test fails CI when step types, schemas, or coreModuleTypes drift apart.
func TestRegistryConsistency(t *testing.T) {
	t.Run("all schema step types in coreModuleTypes", func(t *testing.T) {
		core := make(map[string]bool)
		for _, mt := range schema.KnownModuleTypes() {
			core[mt] = true
		}
		for _, st := range schema.GetStepSchemaRegistry().Types() {
			if !core[st] {
				t.Errorf("step schema %q registered but not in KnownModuleTypes()", st)
			}
		}
	})

	t.Run("template func descriptions cover funcMap", func(t *testing.T) {
		defs := module.TemplateFuncDescriptions()
		if len(defs) < 30 {
			t.Errorf("expected at least 30 template func descriptions, got %d", len(defs))
		}
	})

	t.Run("engine loads all builtin plugins", func(t *testing.T) {
		// Create a minimal engine and verify all plugin step factories are
		// discoverable via the step registry
		e := NewStdEngine(nil)
		types := e.GetStepRegistry().Types()
		if len(types) < 40 {
			t.Errorf("expected at least 40 step types, got %d", len(types))
		}
	})
}
```

The exact implementation depends on the exported API after Phase 2 changes. The agent implementing this should adapt based on what's available.

**Commit:**
```bash
git add consistency_test.go
git commit -m "test: add registry consistency test as CI anti-drift guard

Verifies schema registry, coreModuleTypes, template func descriptions,
and engine step registrations all agree. Fails CI when any source drifts."
```

---

## Execution Order

Tasks can be parallelized within work streams. Cross-stream dependencies:

```
A1 ──┐
A2 ──┤
A3 ──┼──→ G1 (needs all registries fixed)
A4 ──┘
B1 ──────→ G1 (needs interfaces for engine test)
C1 → C2
D1 (independent)
E1-E5 (independent of each other, need working build)
F1 (independent, needs working build)
```

**Parallel groups:**
- Group 1: A1, A2, A4, B1, C1, D1 (all independent)
- Group 2: A3 (depends on A1 removing the old code), C2 (depends on C1)
- Group 3: E1-E5, F1 (independent, after build is green)
- Group 4: G1 (after all other work streams)
