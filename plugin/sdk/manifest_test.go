package sdk

import (
	"strings"
	"sync"
	"testing"
)

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

// TestManifest_IaCProvider_RejectsTypoKey verifies that a typo inside
// iacProvider (e.g., the lowercase "computeplanversion") is rejected by
// the schema rather than silently parsing to a zero-valued IaCProvider —
// which would produce a silent v1 dispatch downgrade. The root object
// stays permissive so plugin.json files with version/author/etc. still
// parse, but iacProvider is strict by design.
func TestManifest_IaCProvider_RejectsTypoKey(t *testing.T) {
	cases := []string{
		`{"name":"x","iacProvider":{"computeplanversion":"v2"}}`, // lowercase typo
		`{"name":"x","iacProvider":{"foo":"bar"}}`,               // unknown key
	}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			_, err := ParseManifest([]byte(in))
			if err == nil {
				t.Errorf("expected schema rejection for %q; got nil", in)
			}
		})
	}
}

// TestManifest_RootPermitsAdditionalProperties verifies that the root
// object accepts unknown top-level keys so existing plugin.json files
// (which carry version/author/dependencies/etc.) parse cleanly through
// the SDK manifest. Pure-additive contract.
func TestManifest_RootPermitsAdditionalProperties(t *testing.T) {
	in := `{"name":"x","version":"1.2.3","author":"jane","description":"hi","iacProvider":{"computePlanVersion":"v2"}}`
	m, err := ParseManifest([]byte(in))
	if err != nil {
		t.Fatalf("expected pass; got %v", err)
	}
	if m.IaCProvider.EffectiveComputePlanVersion() != "v2" {
		t.Errorf("got %q want %q", m.IaCProvider.EffectiveComputePlanVersion(), "v2")
	}
}

// TestManifestSchemaJSON_ReturnsCopy verifies that mutating the slice
// returned from ManifestSchemaJSON cannot affect the embedded schema
// observed by subsequent callers.
func TestManifestSchemaJSON_ReturnsCopy(t *testing.T) {
	a := ManifestSchemaJSON()
	if len(a) == 0 {
		t.Fatal("ManifestSchemaJSON() returned empty bytes")
	}
	a[0] = 0xFF // attempt to corrupt
	b := ManifestSchemaJSON()
	if b[0] == 0xFF {
		t.Error("ManifestSchemaJSON returned a shared slice; embedded schema is mutable from callers")
	}
	if !strings.Contains(string(b), "computePlanVersion") {
		t.Error("ManifestSchemaJSON copy lost the schema body")
	}
}

// TestParseManifest_ConcurrentRaceFree exercises the race detector against
// the lazy schema cache: 32 goroutines call ParseManifest simultaneously
// before any sequential call has populated the cache. Run under -race; a
// failure here means the sync.Once guard around compiledSchema regressed.
//
// Note: this test relies on the package-init ordering — Go's testing
// framework gives each test a fresh goroutine but the package globals
// persist across tests in the same binary, so by the time this test runs
// other tests may already have warmed the cache. The test is still useful
// because it stresses concurrent reads of the cached pointer and any
// internal state inside the jsonschema.Schema.Validate path.
func TestParseManifest_ConcurrentRaceFree(t *testing.T) {
	const goroutines = 32
	const inputs = 4
	manifests := []string{
		`{"name":"a","iacProvider":{}}`,
		`{"name":"b","iacProvider":{"computePlanVersion":"v1"}}`,
		`{"name":"c","iacProvider":{"computePlanVersion":"v2"}}`,
		`{"name":"d","iacProvider":{"computePlanVersion":"v3"}}`, // expected to error
	}
	var wg sync.WaitGroup
	start := make(chan struct{})
	wg.Add(goroutines)
	for i := range goroutines {
		go func(idx int) {
			defer wg.Done()
			<-start // align goroutines for maximum concurrent pressure on loadSchema
			in := manifests[idx%inputs]
			_, _ = ParseManifest([]byte(in))
		}(i)
	}
	close(start)
	wg.Wait()
}
