package interfaces

import "context"

// StepCredentialTarget uses the host's manager registration, not plugin metadata.
type StepCredentialTarget struct {
	Plugin, StepType, StepName string
}

// StepCredentialGrant is explicit operator authority for one literal root field.
type StepCredentialGrant struct {
	Plugin   string `json:"plugin"`
	StepType string `json:"step_type"`
	StepName string `json:"step_name"`
	Field    string `json:"field"`
	Ref      string `json:"ref"`
	Scope    string `json:"scope"`
}

type ResolvedStepCredential struct {
	Ref, Value string
}

// BoundStepCredentials is host-only and must never enter serialized config.
type BoundStepCredentials struct {
	ConfigLookup   func(string) (string, bool)
	ValidateConfig func(map[string]any) error
	Resolve        func(context.Context) ([]ResolvedStepCredential, error)
}

type StepCredentialBinder interface {
	BindStep(StepCredentialTarget, map[string]any) (*BoundStepCredentials, error)
}
