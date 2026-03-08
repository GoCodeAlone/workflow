# wfctl modernize — YAML Config Codemod Tool

## Problem

Users encounter runtime errors from known YAML config anti-patterns that could be detected and fixed automatically. During scenario testing, 8+ recurring issues were identified that require manual debugging and fixing. A codemod tool eliminates this toil.

## Command Interface

```
wfctl modernize [options] <config.yaml|directory>
wfctl modernize --list-rules
wfctl modernize --dry-run config/app.yaml          # default: show what would change
wfctl modernize --apply config/app.yaml             # write changes in-place
wfctl modernize --rules hyphen-steps,conditional-field config/app.yaml
wfctl modernize --exclude-rules validate-syntax config/app.yaml
```

Dry-run is the default. `--apply` writes changes in-place (git is the backup).

## Rules

| Rule ID | Description | Severity | Fixable |
|---------|-------------|----------|---------|
| `hyphen-steps` | Rename hyphenated step names to underscores, update all references | error | yes |
| `conditional-field` | Convert `{{ }}` template syntax in `step.conditional` field to dot-path | error | yes |
| `db-query-mode` | Add `mode: single` to `step.db_query` steps whose downstream templates use `.row` or `.found` | warning | yes |
| `db-query-index` | Convert `.steps.X.row.Y` dot-access to `index .steps "X" "row" "Y"` in templates | error | yes |
| `database-to-sqlite` | Convert `database.workflow` modules to `storage.sqlite` with `dbPath` | warning | yes |
| `absolute-dbpath` | Warn on absolute `dbPath` values in `storage.sqlite` | warning | no |
| `empty-routes` | Detect empty `routes` map in `step.conditional` | error | no |
| `camelcase-config` | Detect snake_case config field names (engine requires camelCase) | warning | no |

## Architecture

### AST-Based Transformations

Uses `yaml.Node` (gopkg.in/yaml.v3) for parsing and rewriting. This preserves comments, formatting, and ordering — critical for config files users maintain by hand. The LSP already uses `yaml.Node` for diagnostics, so this pattern is established.

### Rule Interface

```go
type Rule struct {
    ID          string
    Description string
    Severity    string // "error" or "warning"
    Check       func(root *yaml.Node, cfg *config.WorkflowConfig) []Finding
    Fix         func(root *yaml.Node) []Change
}

type Finding struct {
    RuleID  string
    Line    int
    Column  int
    Message string
    Fixable bool
}

type Change struct {
    RuleID      string
    Line        int
    Description string
}
```

- `Check()` always runs — detects issues and reports findings
- `Fix()` only runs with `--apply` — mutates the `yaml.Node` AST
- After all fixes, the modified AST is marshaled back to the file

### Output Format

**Dry-run (default):**
```
config/app.yaml:
  line 12: [hyphen-steps] Step "check-xss" uses hyphens (fixable)
  line 45: [conditional-field] step.conditional field uses template syntax (fixable)
  line 78: [db-query-mode] step.db_query missing mode:single, downstream uses .row (fixable)

3 issues found (3 fixable). Run with --apply to fix.
```

**Apply mode:**
```
config/app.yaml:
  line 12: [hyphen-steps] Renamed "check-xss" -> "check_xss" (+ 3 references)
  line 45: [conditional-field] Converted field to dot-path syntax
  line 78: [db-query-mode] Added mode: single

3 fixes applied.
```

Supports `--format json` for CI integration.

## Files

| File | Purpose |
|------|---------|
| `cmd/wfctl/modernize.go` | Command entry, flag parsing, orchestration, AST helpers |
| `cmd/wfctl/modernize_rules.go` | All rule implementations |
| `cmd/wfctl/modernize_test.go` | Tests with inline YAML fixtures |

## Key Decisions

- **Dry-run by default** — safe; user must opt in with `--apply`
- **AST-based** — `yaml.Node` preserves comments and formatting better than unmarshal/marshal round-trips
- **Rules are data-driven** — each rule is a struct with check/fix functions, easy to add new rules
- **Directory scanning** — reuses existing `findYAMLFiles()` helper from wfctl
- **No backup files** — git is the backup
- **Follows existing wfctl patterns** — `flag.FlagSet`, `reorderFlags()`, same file conventions
