# Dual-Mode Template System Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add expr-lang/expr as a second expression engine alongside Go templates, using `${ }` syntax for clean expressions while `{{ }}` continues to work.

**Architecture:** New `ExprEngine` wraps expr-lang/expr with pipeline context. `TemplateEngine.Resolve` detects `${ }` vs `{{ }}` and dispatches to the appropriate engine. `skip_if`/`if` fields support both syntaxes. All 30+ existing template functions are registered as expr functions. `wfctl migrate expressions` converts simple Go template expressions to expr syntax.

**Tech Stack:** Go, github.com/expr-lang/expr, text/template (existing)

**Design Doc:** `docs/plans/2026-03-28-ecosystem-restructuring-design.md` (Workstream 5)

---

### Task 1: Add expr-lang/expr dependency and create ExprEngine

**Files:**
- Modify: `go.mod` — add `github.com/expr-lang/expr`
- Create: `module/pipeline_expr.go`
- Create: `module/pipeline_expr_test.go`

The ExprEngine wraps expr-lang/expr with the pipeline context as the expression environment. It evaluates `${ ... }` expressions.

**Step 1:** `go get github.com/expr-lang/expr`

**Step 2:** Write tests for ExprEngine:
- Simple field access: `${ body.name }` → resolves from Current
- Step output access: `${ steps.validate.user_id }` → resolves from StepOutputs
- Boolean expression: `${ steps.check.found == true }` → returns "true"
- Comparison: `${ body.age > 18 }` → returns "true"/"false"
- Arithmetic: `${ body.price * body.quantity }` → returns computed value
- String concatenation: `${ "Hello " + body.name }` → returns string
- Nil/missing field returns empty string (not error)
- Nested map access: `${ steps.fetch.row.email }` → deep map lookup

**Step 3:** Implement ExprEngine:
```go
type ExprEngine struct {
    functions []expr.Option  // registered custom functions
}

func NewExprEngine() *ExprEngine { ... }

func (e *ExprEngine) Evaluate(expression string, pc *PipelineContext) (string, error) {
    env := e.buildEnv(pc)  // map[string]any with steps, body, trigger, meta, current
    program, err := expr.Compile(expression, expr.Env(env), e.functions...)
    result, err := expr.Run(program, env)
    return fmt.Sprintf("%v", result), nil
}

func (e *ExprEngine) buildEnv(pc *PipelineContext) map[string]any {
    // Same shape as templateData() but also expose top-level keys from Current
}
```

**Step 4:** Run tests: `go test ./module/ -run TestExprEngine -v`

**Step 5:** Commit.

---

### Task 2: Register template functions as expr functions

**Files:**
- Modify: `module/pipeline_expr.go` — add function options
- Modify: `module/pipeline_expr_test.go` — test function calls

Register all 30+ existing template functions (from templateFuncMap) as expr custom functions so users get the same capabilities:

- String: `upper()`, `lower()`, `title()`, `replace()`, `contains()`, `hasPrefix()`, `hasSuffix()`, `split()`, `join()`, `trimSpace()`, `urlEncode()`
- Math: `add()`, `sub()`, `mul()`, `div()`
- Type: `toInt()`, `toFloat()`, `toString()`
- Data: `json()`, `default()`, `coalesce()`
- Collections: `sum()`, `pluck()`, `flatten()`, `unique()`, `groupBy()`, `sortBy()`, `first()`, `last()`, `min()`, `max()`
- Identity: `uuid()`, `uuidv4()`
- Time: `now()`
- Config: `config()` — reads from ConfigRegistry

Tests for representative functions:
- `${ upper("hello") }` → "HELLO"
- `${ default(body.missing, "fallback") }` → "fallback"
- `${ sum(pluck(steps.fetch.rows, "amount")) }` → computed sum
- `${ json(body) }` → JSON string
- `${ now("RFC3339") }` → current time

Run: `go test ./module/ -run TestExprFunctions -v`

Commit.

---

### Task 3: Integrate expr into TemplateEngine.Resolve

**Files:**
- Modify: `module/pipeline_template.go` — detect `${ }` and dispatch to ExprEngine
- Modify: `module/pipeline_template_test.go` — test dual-mode resolution

Update `TemplateEngine.Resolve` to handle both syntaxes:

```go
func (te *TemplateEngine) Resolve(tmplStr string, pc *PipelineContext) (string, error) {
    // Check for expr syntax first
    if containsExpr(tmplStr) {
        return te.resolveWithExpr(tmplStr, pc)
    }
    // Existing Go template path
    if !strings.Contains(tmplStr, "{{") {
        return tmplStr, nil
    }
    // ... existing Go template code ...
}

// containsExpr returns true if the string contains ${ ... } expressions
func containsExpr(s string) bool {
    return strings.Contains(s, "${") && strings.Contains(s, "}")
}

// resolveWithExpr evaluates ${ ... } expressions, leaving non-expr text as-is
func (te *TemplateEngine) resolveWithExpr(tmplStr string, pc *PipelineContext) (string, error) {
    // Replace each ${ ... } with evaluated result
    // Handle mixed content: "Hello ${ body.name }, your order is ${ steps.order.id }"
}
```

Also update `ResolveMap` to handle expr in nested maps/slices.

Tests:
- Pure expr: `${ body.name }` → resolved
- Pure Go template: `{{ .body.name }}` → resolved (unchanged behavior)
- Mixed in one string: `"Hello ${ body.name }, order {{ .steps.order.id }}"` — Go templates first, then expr
- Expr in map values: `ResolveMap({"key": "${ body.val }"}, pc)`

Run: `go test ./module/ -run TestResolve -v`

Commit.

---

### Task 4: Add expr support to skip_if and if fields

**Files:**
- Modify: `module/pipeline_step_skip.go` — detect expr in skip_if/if
- Modify: `module/pipeline_step_skip_test.go` — test both syntaxes

Update `SkippableStep.Execute` to support expr syntax in skip_if and if fields:

```go
func (s *SkippableStep) Execute(ctx context.Context, pc *PipelineContext) (*interfaces.StepResult, error) {
    if s.skipIf != "" {
        val, err := s.tmpl.Resolve(s.skipIf, pc)  // Resolve already handles both syntaxes
        if err != nil { val = "" }
        if isTruthy(val) {
            return skippedResult("skip_if evaluated to true"), nil
        }
    }
    // ... same for ifExpr ...
}
```

Since `Resolve` now handles `${ }`, skip_if/if should work automatically. But write explicit tests:

- `skip_if: "${ steps.check.found == true }"` — skips when found is true
- `skip_if: "{{ eq (index .steps \"check\" \"found\") true }}"` — same behavior, Go template syntax
- `if: "${ body.status == \"active\" }"` — executes when active
- `if: "${ body.age > 18 && body.verified == true }"` — compound expression
- Falsy expr results: `${ false }`, `${ 0 }`, `${ "" }` — all falsy

Run: `go test ./module/ -run TestSkippable -v`

Commit.

---

### Task 5: wfctl migrate expressions command

**Files:**
- Create: `cmd/wfctl/migrate_expressions.go`
- Create: `cmd/wfctl/migrate_expressions_test.go`
- Modify: `cmd/wfctl/main.go` — register subcommand

Add `wfctl migrate expressions` that reads a YAML config file and converts simple Go template expressions to expr syntax.

Conversions:
- `{{ .body.name }}` → `${ body.name }`
- `{{ .steps.validate.user_id }}` → `${ steps.validate.user_id }`
- `{{ index .steps "my-step" "field" }}` → `${ steps["my-step"]["field"] }`
- `{{ eq .status "active" }}` → `${ status == "active" }`
- `{{ gt .body.age 18 }}` → `${ body.age > 18 }`
- `{{ and (eq .x "a") (gt .y 5) }}` → `${ x == "a" && y > 5 }`
- Complex expressions with pipes: leave as `{{ }}` with `# TODO: migrate` comment

Flags:
- `-config <file>` — input YAML file
- `-output <file>` — output file (default: stdout)
- `-inplace` — modify file in-place
- `-dry-run` — show what would change without writing

Tests:
- Simple interpolation conversion
- Index syntax conversion
- Comparison operator conversion
- Boolean operator conversion
- Complex expression left unchanged with TODO comment
- Dry-run mode outputs diff

Run: `go test ./cmd/wfctl/ -run TestMigrateExpressions -v`

Commit.

---

### Task 6: LSP hover/completion for expr syntax

**Files:**
- Modify: `lsp/hover.go` — detect `${ }` in YAML values, show expr function docs
- Modify: `lsp/completion.go` — suggest expr functions and context keys inside `${ }`
- Modify: `lsp/hover_test.go` / `lsp/completion_test.go`

When the cursor is inside a `${ ... }` expression in a YAML value:

**Hover:**
- On a function name (e.g., `upper`) → show signature and description
- On `steps.` → show "Pipeline step outputs"
- On `body.` → show "Request body fields"

**Completion:**
- After `${ ` → suggest: `steps`, `body`, `trigger`, `meta`, `config()`, all functions
- After `${ steps.` → suggest known step names from the current pipeline
- After function name → suggest `(` with parameter hints

Tests:
- Hover on `upper` inside `${ upper(x) }` returns function doc
- Completion inside `${ }` includes `steps`, `body`, function names
- No expr completions inside `{{ }}` (Go template territory)

Run: `go test ./lsp/ -run TestExpr -v`

Commit.

---

### Task 7: Documentation and schema updates

**Files:**
- Modify: `docs/dsl-reference.md` — add expr syntax documentation
- Modify: `cmd/wfctl/dsl-reference-embedded.md` — same (keep in sync)
- Modify: `DOCUMENTATION.md` — add expr section to template functions docs

Add a new section to the DSL reference:

```markdown
## Expressions

The workflow engine supports two expression syntaxes:

### Go Templates (legacy)
`{{ .body.name }}` — standard Go text/template syntax

### Expr (recommended for logic)
`${ body.name }` — clean expression syntax powered by expr-lang

Expr is recommended for conditions, comparisons, and data transformation.
Go templates remain supported for simple string interpolation.
```

Document all available expr functions with examples.

Commit.

---

## Summary

| Task | Type | Scope |
|------|------|-------|
| 1 | Core | ExprEngine + tests |
| 2 | Core | Register 30+ functions as expr functions |
| 3 | Core | Integrate into TemplateEngine.Resolve (dual dispatch) |
| 4 | Core | skip_if/if expr support |
| 5 | CLI | wfctl migrate expressions command |
| 6 | LSP | Hover + completion for ${ } syntax |
| 7 | Docs | DSL reference + documentation |
