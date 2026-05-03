package inputsnapshot

import "errors"

// ErrEnvVarChanged is the typed sentinel returned by the apply paths
// (cmd/wfctl/infra.go persisted-`--plan` path in W-1; wfctlhelpers.ApplyPlan
// in-process path in W-3a/T3.1.5) when an env var referenced at plan time
// has a different fingerprint at apply time. Callers can match with
// errors.Is(err, ErrEnvVarChanged) to detect the plan-stale case
// independently of the human-readable per-key drift message.
var ErrEnvVarChanged = errors.New("plan stale: env-var changed since plan")
