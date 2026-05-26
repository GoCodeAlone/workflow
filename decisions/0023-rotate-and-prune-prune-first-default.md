# 0023: `wfctl infra rotate-and-prune` — `--prune-first` default flip

- **Date:** 2026-05-09
- **Status:** Accepted

## Context

`wfctl infra rotate-and-prune` was shipped in v0.27.x with a fixed step
ordering:

1. **Step 1 (rotate):** mint a new canonical credential via the
   provider's `Create` call, push the result to GH Secrets via the
   engine-routing path, and revoke the old credential per ADR 0012.
2. **Step 2 (prune):** delegate to `runInfraPrune` with
   `--created-before=<new.created_at>` + `--exclude-access-key=<new>`
   to delete every older key for the same `--type`, with
   `--preserve-names` as an additive allowlist.

This ordering has a fatal flaw at quota: when the cloud account is at
the per-resource-type cap (e.g., DigitalOcean Spaces enforces a 200-key
limit per project), Step 1 fails with `HTTP 403: key quota exceeded`
before Step 2 ever gets a chance to free quota. The chicken-and-egg:
*the very condition the operator needs to clean up makes the cleanup
tool unusable.*

The recovery path before this ADR was manual: log into the DO console,
hand-delete enough orphan keys to bring the account under quota, then
re-invoke `wfctl infra rotate-and-prune`. That's exactly the
pre-wfctl-rotation workflow the tool was built to eliminate.

## Decision

Add a `--prune-first` boolean flag that flips the step ordering:

- **`--prune-first=true` (NEW DEFAULT):**
  1. **Step 0 (pre-prune):** delete every resource of `--type` whose
     `Name != --name` AND whose `Name` does not match `--preserve-names`.
     This is a NAME-based filter — there is no rotation result yet, so
     the time + access-key filter that `runInfraPrune` uses is not
     applicable. The canonical `--name` is always skipped (the rotate
     step replaces it in-place per ADR 0012).
  2. **Step 1 (rotate):** unchanged from the existing flow.
  3. **Step 2 (post-prune defensive sweep):** unchanged from the
     existing flow. Should be a no-op if Step 0 was complete, but
     covers the case where the canonical name's OLD value (now
     replaced in GH Secrets) is still present in the cloud and should
     be deleted.

- **`--prune-first=false` (OPT-OUT, legacy):** preserves the v0.27.1
  step ordering exactly. Step 1 → Step 2, no pre-prune.

The default IS `--prune-first=true`. The safer behavior is the new
default; the legacy quota-fragile behavior is opt-out. This matches
the workspace mandate "force-strict, no fallbacks" — the strict
behavior is the default, the legacy behavior is opt-out for callers
that need to preserve byte-exact ordering for audit / regression
purposes.

The pre-flight `EnumerateAll` probe + `ErrProviderMethodUnimplemented`
handling (added in v0.27.1) is unchanged: regardless of
`--prune-first` value, if NO loaded provider's plugin actually
implements `EnumerateAll` behind the bridge, the dispatcher errors
loud BEFORE any state is mutated.

## Consequences

- The at-quota chicken-and-egg is closed for the default invocation
  path. Operators no longer need to hand-prune via the cloud console
  before running the tool.

- The pre-prune step uses a different filter shape than the
  post-rotation prune (NAME-based instead of TIME + ACCESS_KEY-based)
  because no rotation result exists yet. This is a deliberate
  asymmetry: pre-prune protects the canonical via name match,
  post-prune protects the new key via access-key match. Both pass
  through the same `--preserve-names` regex.

- Output banners are now numbered Step 0 / Step 1 / Step 2 when
  `--prune-first=true`, and Step 1 / Step 2 when `--prune-first=false`.
  Operators reading the logs will see the additional pre-prune
  narrative on the default path.

- The default flip changes observable behavior: orphan keys
  (Name != canonical, not in preserve regex) created AFTER the rotation
  timestamp are now deleted on the default invocation, where under the
  legacy ordering they survived because the post-rotation time filter
  skipped anything with `created_at >= cutoff`. This is intentional —
  orphans are orphans regardless of when they were created. Callers
  that need the legacy semantics use `--prune-first=false`.

- The two-key opt-in (WFCTL_CONFIRM_PRUNE=1 + `--confirm` flag) gates
  the entire flow including the pre-prune step. The pre-prune helper
  defensively re-checks WFCTL_CONFIRM_PRUNE so it remains safe if
  reused outside `runInfraRotateAndPrune`.

- A pre-prune delete failure aborts BEFORE rotation. No state is
  mutated by the rotate step in that case — better to leave the
  account in its current (partially-pruned) state than to mint a new
  key on a half-cleaned account.

## Alternatives considered

- **Keep `--prune-first=false` as default, add the flag as opt-in:**
  rejected. Contradicts the workspace `feedback_proper_fixes_over_workarounds`
  guidance — making operators discover and explicitly enable the
  safer behavior is a workaround, not a fix. The chicken-and-egg
  failure mode is the common case for any account that has accumulated
  orphan keys, which is most accounts running the rotation cadence
  this tool was built for.

- **Detect quota exhaustion in Step 1 and auto-retry with pre-prune:**
  rejected. Couples Step 1's error path to provider-specific quota
  error parsing. The error shape is not standardized across cloud
  providers (DO returns HTTP 403 with a body string, AWS returns
  `LimitExceededException`, Azure returns `QuotaExceeded`). A flag
  is provider-agnostic and easier to reason about.

- **Split into `wfctl infra prune-orphans` + `wfctl infra
  rotate-and-prune`:** rejected. Two-command UX shifts the
  ordering decision to the operator, who has to know to call both in
  sequence. The all-in-one semantic of `rotate-and-prune` is the
  feature; flipping its internal ordering preserves that semantic.

- **Make `--prune-first` an enum (`{first, after, both, none}`):**
  rejected. YAGNI. The two real states are "safe at quota" (default)
  and "legacy ordering" (opt-out for byte-exact regressions). Adding
  enum values for hypothetical future variants creates surface area
  without a use case.

## Related

- `cmd/wfctl/infra_rotate_and_prune.go` — implementation; pre-prune
  helper is `runPreRotationPrune`.
- `cmd/wfctl/infra_rotate_and_prune_test.go` —
  `TestRotateAndPrune_PruneFirst_HappyPath_AtQuota`,
  `TestRotateAndPrune_PruneFirst_DefaultTrue`,
  `TestRotateAndPrune_PruneFirst_False_LegacyOrder`,
  `TestRotateAndPrune_PruneFirst_PreservesCanonicalName`.
- ADR 0012 — provider credential rotation (mint-new-then-revoke-old).
- ADR 0017 — `wfctl infra prune` two-key opt-in (the post-rotation
  prune the defensive sweep delegates to).
- ADR 0020 — storage filter sidecar metadata (RotationResult shape).
- Workspace memory `feedback_proper_fixes_over_workarounds` — proper
  fixes over easy workarounds (the default flip IS the proper fix).
