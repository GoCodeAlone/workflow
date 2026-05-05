# Active test skips

Tests intentionally guarded with `t.Skip` because the underlying behaviour
is a known bug or pending feature. Each row links to the tracking issue
and the trigger for removing the skip.

When a fix lands, drop the `t.Skip(...)` line in the named test and
delete the matching row here in the same PR.

| Test | Issue | Plan to remove |
|------|-------|----------------|

<!--
Currently no active skips.

Removed entry (2026-05-05): the diagnostic test for workflow#540
(`plugin/sdk.TestManifest_IaCProvider_AdditionalPropertiesFalse_IsEnforced`)
was originally proposed in `t.Skip` shape per plan rev3 §I-5, but
empirical verification during PR #553 showed the bug does not reproduce
against the current jsonschema/v6 build. The test was promoted to an
assertive regression guard on the canonical issue inputs (`name`,
`resourceTypes`, `configSchema` plus a synthetic key) so any future
regression turns CI red, and the runbook entry was retired alongside.
workflow#540 stays open until the upstream investigation confirms the
schema-loader behaviour is correct across all draft dialects.
-->
