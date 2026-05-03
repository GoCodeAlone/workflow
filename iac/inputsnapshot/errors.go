package inputsnapshot

import "errors"

// ErrEnvVarChanged is the typed sentinel returned by the apply paths
// (cmd/wfctl/infra.go persisted-`--plan` path in W-1; wfctlhelpers.ApplyPlan
// in-process path in W-3a/T3.1.5) when an env var referenced at plan time
// has a different fingerprint at apply time. Callers match with
// errors.Is(err, ErrEnvVarChanged) to detect the plan-stale case
// programmatically; the user-facing "plan stale: ..." prefix is owned by
// FormatStaleError, so this sentinel deliberately uses a short
// machine-only marker to avoid duplicating that prefix when wrapped via
// fmt.Errorf("%w\n%s", ErrEnvVarChanged, FormatStaleError(...)).
var ErrEnvVarChanged = errors.New("env-var changed since plan")
