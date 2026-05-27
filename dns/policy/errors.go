package policy

import "errors"

var (
	ErrMultipleDefaults = errors.New("dnspolicy: multiple RRs set d=true")
	ErrEmptyOwner       = errors.New("dnspolicy: o= field is empty")
	// ErrUnknownHeritage is reserved for future use. Parse() currently silently
	// skips RRs with unknown heritage values to preserve forward-compatibility;
	// a future stricter parser variant may return this error.
	ErrUnknownHeritage = errors.New("dnspolicy: unknown heritage value (parser ignored RR)")
)

const HeritageV1 = "wfinfra-v1"
