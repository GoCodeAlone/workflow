package sdk

import "testing"

// TestManifest_IaCProvider_ComputePlanVersion exercises the new
// iacProvider.computePlanVersion field. Cases:
//   - default-v1:  field omitted → EffectiveComputePlanVersion() == "v1"
//   - explicit-v1: "v1" → "v1"
//   - explicit-v2: "v2" → "v2"
//   - rejected:    "v3" → ParseManifest returns an error (schema-rejected)
func TestManifest_IaCProvider_ComputePlanVersion(t *testing.T) {
	cases := map[string]struct {
		in      string
		want    string
		wantErr bool
	}{
		"default-v1":  {`{"name":"x","iacProvider":{}}`, "v1", false},
		"explicit-v1": {`{"name":"x","iacProvider":{"computePlanVersion":"v1"}}`, "v1", false},
		"explicit-v2": {`{"name":"x","iacProvider":{"computePlanVersion":"v2"}}`, "v2", false},
		"rejected":    {`{"name":"x","iacProvider":{"computePlanVersion":"v3"}}`, "", true},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			m, err := ParseManifest([]byte(c.in))
			if (err != nil) != c.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, c.wantErr)
			}
			if !c.wantErr && m.IaCProvider.EffectiveComputePlanVersion() != c.want {
				t.Errorf("got %q want %q", m.IaCProvider.EffectiveComputePlanVersion(), c.want)
			}
		})
	}
}

// TestManifest_IaCProvider_ComputePlanVersion_ZeroValue verifies that an
// IaCProvider with the zero value (empty string) reports v1, matching the
// "default-v1" case but exercising the accessor on a Go-zero-valued struct
// (no JSON involved).
func TestManifest_IaCProvider_ComputePlanVersion_ZeroValue(t *testing.T) {
	var p IaCProvider
	if got := p.EffectiveComputePlanVersion(); got != "v1" {
		t.Errorf("zero IaCProvider.EffectiveComputePlanVersion() = %q, want %q", got, "v1")
	}
}
