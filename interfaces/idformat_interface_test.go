package interfaces

import "testing"

// Compile-time check: a concrete type implementing ProviderIDValidator
// satisfies the interface.
var _ ProviderIDValidator = (*fakeValidator)(nil)

type fakeValidator struct{ format ProviderIDFormat }

func (f *fakeValidator) ProviderIDFormat() ProviderIDFormat { return f.format }

func TestProviderIDFormat_ZeroValue(t *testing.T) {
	var f ProviderIDFormat
	if f != IDFormatUnknown {
		t.Errorf("zero value should be IDFormatUnknown, got %v", f)
	}
}

func TestProviderIDFormat_StringRoundtrip(t *testing.T) {
	cases := []struct {
		f    ProviderIDFormat
		name string
	}{
		{IDFormatUnknown, "unknown"},
		{IDFormatUUID, "uuid"},
		{IDFormatDomainName, "domain_name"},
		{IDFormatARN, "arn"},
		{IDFormatFreeform, "freeform"},
	}
	for _, c := range cases {
		if got := c.f.String(); got != c.name {
			t.Errorf("(%v).String() = %q, want %q", c.f, got, c.name)
		}
	}
}
