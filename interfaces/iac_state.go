package interfaces

import (
	"context"
	"time"
)

// IaCLockHandle is returned by IaCStateStore.Lock and is used to release the lock.
type IaCLockHandle interface {
	Unlock(ctx context.Context) error
}

// IaCStateStore provides persistent state tracking for managed resources.
type IaCStateStore interface {
	SaveResource(ctx context.Context, state ResourceState) error
	GetResource(ctx context.Context, name string) (*ResourceState, error)
	ListResources(ctx context.Context) ([]ResourceState, error)
	DeleteResource(ctx context.Context, name string) error

	SavePlan(ctx context.Context, plan IaCPlan) error
	GetPlan(ctx context.Context, id string) (*IaCPlan, error)

	Lock(ctx context.Context, resource string, ttl time.Duration) (IaCLockHandle, error)

	Close() error
}

// ResourceState is the persisted state record for a single managed resource.
type ResourceState struct {
	ID             string         `json:"id"`
	Name           string         `json:"name"`
	Type           string         `json:"type"`
	Provider       string         `json:"provider"`
	ProviderRef    string         `json:"provider_ref,omitempty"`
	ProviderID     string         `json:"provider_id"`
	ConfigHash     string         `json:"config_hash"`
	AppliedConfig  map[string]any `json:"applied_config"`
	Outputs        map[string]any `json:"outputs"`
	Dependencies   []string       `json:"dependencies"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
	LastDriftCheck time.Time      `json:"last_drift_check,omitempty"`
}

// IaCPlan is the complete execution plan for a set of infrastructure changes.
type IaCPlan struct {
	ID        string       `json:"id"`
	Actions   []PlanAction `json:"actions"`
	CreatedAt time.Time    `json:"created_at"`
	// DesiredHash is a SHA-256 hex digest of the canonical desired-state inputs
	// (sorted ResourceSpecs) at the time the plan was generated. wfctl infra apply
	// --plan compares this against the current config to detect stale plans.
	DesiredHash string `json:"plan_hash,omitempty"`

	// SchemaVersion is bumped when on-disk plan format changes (W-5 sets to 2 when JIT is required).
	SchemaVersion int `json:"schema_version,omitempty"`

	// InputSnapshot records env var names read during ${VAR} substitution at
	// plan time, fingerprinting only those that were SET (16-hex-char sha256
	// prefix of the value). Unset vars are omitted from the map; their absence
	// at apply time is therefore not flagged as drift. Apply re-computes inputs
	// and prints diagnostic on mismatch.
	//
	// Completeness caveat: the cmd/wfctl scanner that populates this map
	// (cmd/wfctl/infra_inputsnapshot.go::collectInfraEnvVarRefs) currently
	// walks each module's own Config but does not apply top-level
	// environments[env].envVars defaults. Vars that originate solely from a
	// top-level envVars default may therefore be absent from the snapshot;
	// closing this gap is tracked as a follow-up to W-1.
	InputSnapshot map[string]string `json:"input_snapshot,omitempty"`
}

// PlanAction is a single planned change within an IaCPlan.
type PlanAction struct {
	Action   string         `json:"action"` // create, update, replace, delete
	Resource ResourceSpec   `json:"resource"`
	Current  *ResourceState `json:"current,omitempty"`
	Changes  []FieldChange  `json:"changes,omitempty"`

	// ResolvedConfigHash is the SHA-256 of POST-substitution Resource.Config,
	// computed via platform.ConfigHash. Encoded as lower-case hex (no
	// "sha256:" prefix); empty string when the config map is empty
	// (platform.ConfigHash short-circuit). The field uses `omitempty`, so the
	// empty-string case is ABSENT from plan.json — consumers should treat
	// "key missing" and "value == empty string" as the same condition.
	//
	// Currently populated by ComputePlan and persisted in plan.json so apply
	// has the per-action hash available; the apply-time consumer that surfaces
	// a per-resource diagnostic on mismatch is wired in a follow-up PR (W-3a/
	// T3.1.5). Until then the field is observable via plan.json inspection but
	// not yet enforced at apply.
	ResolvedConfigHash string `json:"resolved_config_hash,omitempty"`
}

// DriftEntry names a single env-var whose fingerprint changed between plan-time
// and apply-time. Used by both the persisted-`--plan` path (cmd/wfctl/infra.go,
// wired in T1.5) and the in-process apply path (wfctlhelpers.ApplyPlan, wired
// in T3.1.5 — both via inputsnapshot.FormatStaleError).
type DriftEntry struct {
	Name             string `json:"name"`
	PlanFingerprint  string `json:"plan_fingerprint"`
	ApplyFingerprint string `json:"apply_fingerprint"`
}

// ApplyResult summarises the outcome of applying a plan.
type ApplyResult struct {
	PlanID    string           `json:"plan_id"`
	Resources []ResourceOutput `json:"resources"`
	Errors    []ActionError    `json:"errors,omitempty"`

	// InitialInputSnapshot captures env-var fingerprints at the start of apply.
	// Populated by wfctlhelpers.ApplyPlan (T3.1) at apply entry by snapshotting
	// every name listed in plan.InputSnapshot through inputsnapshot.OSEnvProvider.
	// Used by the deferred postcondition (T3.1.5) to compute the drift report
	// against the apply-time snapshot.
	InitialInputSnapshot map[string]string `json:"initial_input_snapshot,omitempty"`

	// InputDriftReport names env-vars whose fingerprint changed between plan
	// and apply. Populated by the deferred postcondition in
	// wfctlhelpers.ApplyPlan (T3.1.5) regardless of whether the apply
	// itself succeeded or errored. Empty (or nil) means no drift detected.
	// Entries are sorted by Name for deterministic comparison and stable
	// diagnostic output.
	InputDriftReport []DriftEntry `json:"input_drift_report,omitempty"`

	// ReplaceIDMap propagates new ProviderIDs from Replace actions to
	// dependent resources whose Apply runs later in the same plan.
	// Populated by doReplace (T3.4); consumed by JIT substitution in W-5
	// (T5.2/T5.3). Keyed by the dependent resource Name; the value is the
	// new ProviderID returned from the post-Delete Create.
	ReplaceIDMap map[string]string `json:"replace_id_map,omitempty"`
}

// DestroyResult summarises the outcome of a destroy operation.
type DestroyResult struct {
	Destroyed []string      `json:"destroyed"`
	Errors    []ActionError `json:"errors,omitempty"`
}

// ActionError captures a resource-level error during apply or destroy.
type ActionError struct {
	Resource string `json:"resource"`
	Action   string `json:"action"`
	Error    string `json:"error"`
}
