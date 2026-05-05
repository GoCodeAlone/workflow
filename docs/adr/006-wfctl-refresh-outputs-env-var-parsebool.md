# ADR 006: WFCTL_REFRESH_OUTPUTS uses strconv.ParseBool, not bare presence

## Status

Accepted

## Context

W-2 Task T2.3 (`docs/plans/2026-05-03-iac-conformance-and-replace.md`, line 1061) specified the apply-time pre-step opt-in gate as a bare presence check:

```go
if os.Getenv("WFCTL_REFRESH_OUTPUTS") != "" {
    // run pre-step
}
```

Under that contract, any non-empty value enables the pre-step — including values an operator would universally read as disabling: `0`, `false`, `no`, `off`. T2.3's first quality review (code-reviewer) flagged this as a foot-gun: operators reach for the `=0` / `=false` convention to disable a feature, and the bare-presence check turns that convention into the opposite of its intent. The original commit body also leaned into the wrong mental model by listing `"yes"` as an enabling example, reinforcing the misread that the value was being parsed.

`--skip-refresh` exists as a robust off-switch override, so an operator who falls into the foot-gun has an escape hatch — but discovering it requires reading the docs after the surprise hit, by which point a planner has already burned a `Read` round-trip on every state entry.

## Decision

Implement the gate using `strconv.ParseBool` instead of bare presence. The accept set is well-known to Go developers and matches every common operator convention:

- Truthy enables: `1`, `t`, `T`, `TRUE`, `true`, `True`
- Falsey explicitly disables: `0`, `f`, `F`, `FALSE`, `false`, `False`
- Empty / unset / unrecognised: disabled (default; never an exception)

`--skip-refresh` continues to override the env var unconditionally, preserving the CI escape-hatch contract for environments where the env var is forced on globally.

## Consequences

**Positive:**

- Operator surprise eliminated: `WFCTL_REFRESH_OUTPUTS=0` disables, as expected.
- Self-documenting contract: `ParseBool` is a familiar Go idiom; the godoc on `applyPreStepRefreshEnabled` enumerates the accept set.
- Future-proofed against the same bug class for other refresh-outputs related env vars added in W-3+.

**Negative:**

- Plan-deviation: the implementation diverges from `docs/plans/2026-05-03-iac-conformance-and-replace.md` line 1061's literal contract. Recorded in this ADR so future contributors see the rejected alternative and the reasoning, rather than discovering it via `git blame` archaeology. A future plan revision should reflect the actual implementation; out of scope for this ADR.
- Operators who set the env var to an unrecognised string (e.g. `WFCTL_REFRESH_OUTPUTS=enabled`) silently get the disabled default. The accept set is intentionally narrow; the alternative — accepting any non-empty as truthy — is exactly the foot-gun this ADR rejects. Mitigated by the doc table in `docs/WFCTL.md` enumerating the truthy/falsey values explicitly.

**Provenance:** decided by Claude (autonomous-pipeline team-lead) after code-review/spec-review consensus on 2026-05-04. code-reviewer surfaced the foot-gun during T2.3 quality review; implementer prepared the ParseBool change; spec-reviewer requested this ADR post-hoc; team-lead approved option-1 (approve-as-is + follow-up ADR) over plan revert.

## References

- Plan task spec: `docs/plans/2026-05-03-iac-conformance-and-replace.md` §T2.3 (line 1061)
- Implementation: `cmd/wfctl/infra_apply_refresh_pre.go::applyPreStepRefreshEnabled` (commit `bfd1bbe`)
- Test pinning the falsey-disables semantic: `cmd/wfctl/infra_apply_refresh_pre_test.go::TestApply_PreStepRefresh_EnvVarFalseyValueDisables`
- Operator-facing docs: `docs/WFCTL.md` → `infra refresh-outputs` → "Apply-time pre-step (opt-in)"
