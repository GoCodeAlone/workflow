# Active test skips

Tests intentionally guarded with `t.Skip` because the underlying behaviour
is a known bug or pending feature. Each row links to the tracking issue
and the trigger for removing the skip.

When a fix lands, drop the `t.Skip(...)` line in the named test and
delete the matching row here in the same PR.

| Test | Issue | Plan to remove |
|------|-------|----------------|
| `plugin/sdk.TestManifest_iacProviderAdditionalPropertiesFalse_IsEnforced` | [workflow#540](https://github.com/GoCodeAlone/workflow/issues/540) | Remove `t.Skip` line in fix follow-up PR once the jsonschema-library investigation completes (per plan `2026-05-05-iac-deferred-cleanup` PR 3). |
