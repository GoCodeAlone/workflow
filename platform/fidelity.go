package platform

// FidelityLevel indicates how faithfully a provider implements a capability.
// Providers may not support all capabilities at full fidelity; this type
// makes gaps explicit so users can make informed decisions.
type FidelityLevel string

const (
	// FidelityFull indicates production-equivalent implementation.
	FidelityFull FidelityLevel = "full"

	// FidelityPartial indicates the capability works but with limitations.
	FidelityPartial FidelityLevel = "partial"

	// FidelityStub indicates a mock or no-op implementation (e.g., IAM on local).
	FidelityStub FidelityLevel = "stub"

	// FidelityNone indicates the capability is not supported by this provider.
	FidelityNone FidelityLevel = "none"
)

// FidelityReport is returned during plan generation to inform users of
// capability gaps in the current provider. It identifies which properties
// of a capability are not fully implemented.
type FidelityReport struct {
	// Capability is the abstract capability type being reported on.
	Capability string `json:"capability"`

	// Provider is the provider name.
	Provider string `json:"provider"`

	// Fidelity is the overall fidelity level for this capability.
	Fidelity FidelityLevel `json:"fidelity"`

	// Gaps lists the specific properties that are not fully implemented.
	Gaps []FidelityGap `json:"gaps"`
}

// FidelityGap describes a specific property that is not fully implemented
// by a provider for a given capability.
type FidelityGap struct {
	// Property is the capability property that has a fidelity gap.
	Property string `json:"property"`

	// Description explains what is missing or different.
	Description string `json:"description"`

	// Workaround describes what the provider does instead, if anything.
	Workaround string `json:"workaround,omitempty"`
}

// HasGaps returns true if the fidelity report contains any gaps.
func (fr *FidelityReport) HasGaps() bool {
	return len(fr.Gaps) > 0
}

// IsFullFidelity returns true if the fidelity level is Full with no gaps.
func (fr *FidelityReport) IsFullFidelity() bool {
	return fr.Fidelity == FidelityFull && !fr.HasGaps()
}

// WorseOf returns the lower fidelity level between two levels.
// The ordering from best to worst is: Full > Partial > Stub > None.
func WorseOf(a, b FidelityLevel) FidelityLevel {
	order := map[FidelityLevel]int{
		FidelityFull:    3,
		FidelityPartial: 2,
		FidelityStub:    1,
		FidelityNone:    0,
	}
	if order[a] <= order[b] {
		return a
	}
	return b
}
