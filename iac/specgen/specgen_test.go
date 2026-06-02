package specgen_test

import (
	"bytes"
	"reflect"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/GoCodeAlone/workflow/iac/specgen"
	"github.com/GoCodeAlone/workflow/iac/specparse"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// TestSpecToYAML_RoundTrip asserts that serialising a []interfaces.ResourceSpec
// with SpecToYAML and then re-parsing the YAML bytes (decode to []any →
// specparse.ParseResourceSpecs) produces a slice that deep-equals the input.
func TestSpecToYAML_RoundTrip(t *testing.T) {
	input := []interfaces.ResourceSpec{
		{
			Name: "web-server",
			Type: "droplet",
			Size: interfaces.Size("s-1vcpu-1gb"),
			Config: map[string]any{
				"region":   "nyc3",
				"password": "secret://vault/db-password",
				"tags": []any{
					"env:prod",
					"team:backend",
				},
			},
		},
		{
			Name: "db",
			Type: "database",
		},
	}

	data, err := specgen.SpecToYAML(input)
	if err != nil {
		t.Fatalf("SpecToYAML error: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("SpecToYAML returned empty bytes")
	}

	// Decode YAML bytes → []any, then ParseResourceSpecs.
	var raw []any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		t.Fatalf("yaml.Unmarshal error: %v\nYAML:\n%s", err, data)
	}

	got, err := specparse.ParseResourceSpecs(raw)
	if err != nil {
		t.Fatalf("ParseResourceSpecs error: %v", err)
	}

	if !reflect.DeepEqual(got, input) {
		t.Errorf("round-trip mismatch.\ngot:  %+v\nwant: %+v", got, input)
	}
}

// TestSpecToYAML_PreservesSecretRefs asserts that secret:// references are
// emitted verbatim in the serialised YAML (not expanded or redacted).
func TestSpecToYAML_PreservesSecretRefs(t *testing.T) {
	specs := []interfaces.ResourceSpec{
		{
			Name: "web",
			Type: "droplet",
			Config: map[string]any{
				"password": "secret://vault/db-password",
			},
		},
	}

	data, err := specgen.SpecToYAML(specs)
	if err != nil {
		t.Fatalf("SpecToYAML error: %v", err)
	}

	const wantRef = "secret://vault/db-password"
	if !bytes.Contains(data, []byte(wantRef)) {
		t.Errorf("YAML output does not contain %q\nYAML:\n%s", wantRef, data)
	}
}
