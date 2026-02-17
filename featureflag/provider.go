package featureflag

import (
	"context"
	"encoding/json"
	"time"
)

// FlagType enumerates the supported feature flag value types.
type FlagType string

const (
	FlagTypeBoolean FlagType = "boolean"
	FlagTypeString  FlagType = "string"
	FlagTypeInteger FlagType = "integer"
	FlagTypeFloat   FlagType = "float"
	FlagTypeJSON    FlagType = "json"
)

// EvaluationContext holds the contextual data used to evaluate a feature flag.
// The UserKey uniquely identifies the subject (user, service, etc.). Attributes
// carry additional targeting information such as email, groups, plan, etc.
type EvaluationContext struct {
	UserKey    string            `json:"user_key"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

// FlagValue represents the evaluated result of a single feature flag.
type FlagValue struct {
	Key      string          `json:"key"`
	Value    any             `json:"value"`
	Type     FlagType        `json:"type"`
	Source   string          `json:"source"`             // provider name that produced the value
	Reason   string          `json:"reason,omitempty"`   // why this value was selected
	Metadata json.RawMessage `json:"metadata,omitempty"` // provider-specific extra data
}

// FlagMeta describes a flag definition without evaluating it.
type FlagMeta struct {
	Key         string    `json:"key"`
	Type        FlagType  `json:"type"`
	Description string    `json:"description,omitempty"`
	Enabled     bool      `json:"enabled"`
	Tags        []string  `json:"tags,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// FlagChangeEvent is emitted when a flag value changes within a provider.
type FlagChangeEvent struct {
	Key    string   `json:"key"`
	Value  any      `json:"value"`
	Type   FlagType `json:"type"`
	Source string   `json:"source"`
}

// Provider is the interface that all feature-flag backends must implement.
type Provider interface {
	// Name returns a unique identifier for this provider (e.g. "generic", "launchdarkly").
	Name() string

	// Evaluate returns the resolved flag value for the given key and context.
	// Returns an error if the flag does not exist or evaluation fails.
	Evaluate(ctx context.Context, key string, evalCtx EvaluationContext) (FlagValue, error)

	// AllFlags returns the evaluated values for every flag visible to the given context.
	AllFlags(ctx context.Context, evalCtx EvaluationContext) ([]FlagValue, error)

	// Subscribe registers a callback that is invoked whenever a flag changes.
	// The returned function cancels the subscription.
	Subscribe(fn func(FlagChangeEvent)) (cancel func())
}
