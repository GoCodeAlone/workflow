# Active test skips

Tests intentionally guarded with `t.Skip` because the underlying behaviour
is a known bug or pending feature. Each row links to the tracking issue
and the trigger for removing the skip.

When a fix lands, drop the `t.Skip(...)` line in the named test and
delete the matching row here in the same PR.

| Test | Issue | Plan to remove |
|------|-------|----------------|
| `plugin/sdk.TestManifest_IaCProviderAdditionalPropertiesFalse_IsEnforced` | [workflow#540](https://github.com/GoCodeAlone/workflow/issues/540) | Behaviour-probed skip: the test SKIPs only while `ParseManifest` accepts the canonical bug input and PASSes (silent regression guard) when the bug is fixed. No source change required when the fix lands; this row exists so the SKIP is grep-able while it triggers. Drop the row when workflow#540 closes. |
