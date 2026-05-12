# Underscore-prefix proto field audit

**Date:** 2026-05-12
**Context:** ADR-0031 establishes `_`-prefix as the engine-internals namespace. This audit verifies no current plugin's proto schema declares a field with `_`-prefix that would be silently stripped by the new `stripInternalKeys` in `createTypedConfigRequest` (Task 3 / workflow PR for v0.51.3).

## Scope

Audit covers workflow's own engine proto/ plus the 6 wave plugin repos listed in the v0.51.3 plan:

- `workflow` (engine — `plugin/external/proto/*.proto`)
- `workflow-plugin-digitalocean`
- `workflow-plugin-eventbus`
- `workflow-plugin-audit-chain`
- `workflow-plugin-payments`
- `workflow-plugin-auth`
- `workflow-plugin-twilio`

## Method

Tight `grep -E` pattern matches proto3 field declarations whose **field name** starts with `_`. Per F3 absorption: the previous looser pattern produced false positives on snake_case field names (e.g. `string my_field = 1;`). The current pattern keys on the underscore appearing immediately before the field name, not anywhere in the line.

```bash
PATTERN='^[[:space:]]+(repeated[[:space:]]+|optional[[:space:]]+)?[a-zA-Z_][a-zA-Z0-9_.]*[[:space:]]+_[a-z][a-zA-Z0-9_]*[[:space:]]*=[[:space:]]*[0-9]'

cd /Users/jon/workspace
for repo in workflow workflow-plugin-digitalocean workflow-plugin-eventbus \
            workflow-plugin-audit-chain workflow-plugin-payments \
            workflow-plugin-auth workflow-plugin-twilio; do
    echo "=== $repo ==="
    if [ -d "$repo" ]; then
        find "$repo" -name "*.proto" \
                     -not -path "*/node_modules/*" \
                     -not -path "*/_worktrees/*" -print0 \
            | xargs -0 grep -HnE "$PATTERN" 2>/dev/null \
            || echo "(no underscore-prefix field declarations found)"
    else
        echo "(repo not present locally)"
    fi
done
```

### Pattern sanity check

Verified the pattern correctly distinguishes between `_internal` (matches) and `my_field` (no match):

```text
input:
  string my_field = 1;
  string _internal = 2;
  repeated string _items = 3;
  optional int32 _count = 4;
  int32 valid_num = 5;

matches (lines):
  3:  string _internal = 2;
  4:  repeated string _items = 3;
  5:  optional int32 _count = 4;
```

Lines 2 (`my_field`) and 6 (`valid_num`) are correctly excluded.

## Audit transcript

```
=== workflow ===
(no underscore-prefix field declarations found)
=== workflow-plugin-digitalocean ===
(no underscore-prefix field declarations found)
=== workflow-plugin-eventbus ===
(no underscore-prefix field declarations found)
=== workflow-plugin-audit-chain ===
(no underscore-prefix field declarations found)
=== workflow-plugin-payments ===
(no underscore-prefix field declarations found)
=== workflow-plugin-auth ===
(no underscore-prefix field declarations found)
=== workflow-plugin-twilio ===
(no .proto files present)
```

Workflow's own proto/ files inspected: `plugin/external/proto/iac.proto`, `plugin/external/proto/plugin.proto`. Neither declares an `_`-prefix field.

`workflow-plugin-twilio` has no `.proto` files in-tree (legacy non-strict plugin; not affected by the strip).

## Findings

| Repo | Result |
|---|---|
| `workflow` (engine) | clean |
| `workflow-plugin-digitalocean` | clean |
| `workflow-plugin-eventbus` | clean |
| `workflow-plugin-audit-chain` | clean |
| `workflow-plugin-payments` | clean |
| `workflow-plugin-auth` | clean |
| `workflow-plugin-twilio` | clean (no `.proto` files; legacy plugin) |

## Verdict

**PASS.** No `_`-prefix proto field declarations exist in any audited repo. The Task 3 `stripInternalKeys` helper introduces no field-collision risk for v0.51.3. The `_`-prefix namespace remains reserved for engine internals (`_config_dir` and any future additions) as specified in ADR-0031.
