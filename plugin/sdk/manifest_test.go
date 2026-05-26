package sdk

import (
	"errors"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"
)

// TestManifest_IaCProvider_ComputePlanVersion exercises the
// iacProvider.computePlanVersion field at the schema layer. Per
// workflow#699 the EffectiveComputePlanVersion accessor is gone (the
// authoritative gate is now the typed CapabilitiesResponse check in
// cmd/wfctl/deploy_providers.go); the manifest field remains as a
// parse-time validation surface. Cases:
//   - omitted:     accepted (manifest field is optional)
//   - explicit-v1: accepted (advisory; the runtime gate rejects v1)
//   - explicit-v2: accepted
//   - rejected:    "v3" → ParseManifest returns an error (schema-rejected)
func TestManifest_IaCProvider_ComputePlanVersion(t *testing.T) {
	cases := map[string]struct {
		in      string
		want    string
		wantErr bool
	}{
		"omitted":     {`{"name":"x","iacProvider":{}}`, "", false},
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
			if !c.wantErr && m.IaCProvider.ComputePlanVersion != c.want {
				t.Errorf("got %q want %q", m.IaCProvider.ComputePlanVersion, c.want)
			}
		})
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
	if m.IaCProvider.ComputePlanVersion != "v2" {
		t.Errorf("got %q want %q", m.IaCProvider.ComputePlanVersion, "v2")
	}
}

// TestManifest_IaCProvider_AdditionalPropertiesFalse_IsEnforced is an
// active regression guard for workflow#540.
//
// Background: workflow#540 surfaced from P-DO PR #61 as "the
// `iacProvider` block accepts extra keys (`name`, `resourceTypes`,
// `configSchema`) without validation error, despite the schema
// declaring `additionalProperties: false`." The bug does not reproduce
// against the current `jsonschema/v6` build (verified empirically with
// `ParseManifest` rejecting the canonical fixtures during the test
// authoring); this test enforces the contract going forward so any
// future regression — library upgrade, schema-loader change, draft
// dialect drift — turns CI red on the same canonical inputs that
// motivated the issue.
//
// Fixtures cover the exact key names cited in the issue
// (`name`, `resourceTypes`, `configSchema`) plus a synthetic key, since
// the bug surfaced on real `iacProvider` content rather than on
// arbitrary unknown keys. Each case must produce a `*jsonschema.ValidationError`
// whose causes tree contains a `*kind.AdditionalProperties` entry naming
// the offending keys — asserting against the structured ErrorKind
// rather than against English error wording so the test does not break
// when the library localises or rewords its messages (Copilot review on
// workflow#553, commit e0ae98b).
//
// SHAPE: assertive regression guard. The plan rev3 §I-5 alt-shape
// originally specified `t.Skip` because the bug was assumed live on
// main; that assumption did not hold (Copilot review on workflow#553,
// commit 6563b57). With the bug not reproducing today, the assertive
// shape is strictly stronger — any future regression fails CI loudly
// with a clear pointer to workflow#540.
func TestManifest_IaCProvider_AdditionalPropertiesFalse_IsEnforced(t *testing.T) {
	cases := map[string]struct {
		manifest string
		// wantKeys is the set of extra iacProvider keys the test expects
		// the schema validator to flag with an additionalProperties
		// rejection. Asserting against the structured ErrorKind tree
		// means the assertion does not depend on the library's English
		// error wording — only on the contractual behaviour ("the
		// `additionalProperties` keyword fired on these specific keys").
		wantKeys []string
	}{
		"issue-name": {
			manifest: `{"name":"test-plugin","iacProvider":{"computePlanVersion":"v2","name":"do"}}`,
			wantKeys: []string{"name"},
		},
		"issue-resourceTypes": {
			manifest: `{"name":"test-plugin","iacProvider":{"computePlanVersion":"v2","resourceTypes":["droplet"]}}`,
			wantKeys: []string{"resourceTypes"},
		},
		"issue-configSchema": {
			manifest: `{"name":"test-plugin","iacProvider":{"computePlanVersion":"v2","configSchema":{}}}`,
			wantKeys: []string{"configSchema"},
		},
		"issue-all-three": {
			manifest: `{"name":"test-plugin","iacProvider":{"computePlanVersion":"v2","name":"do","resourceTypes":["droplet"],"configSchema":{}}}`,
			wantKeys: []string{"name", "resourceTypes", "configSchema"},
		},
		"synthetic-extra-key": {
			manifest: `{"name":"test-plugin","iacProvider":{"computePlanVersion":"v2","bogusKeyThatShouldBeRejected":"value"}}`,
			wantKeys: []string{"bogusKeyThatShouldBeRejected"},
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ParseManifest([]byte(c.manifest))
			if err == nil {
				t.Fatalf("workflow#540 regressed: ParseManifest accepted extra iacProvider key(s) %v; expected schema rejection",
					c.wantKeys)
			}
			// Walk the *jsonschema.ValidationError tree and collect
			// every key that triggered an `additionalProperties`
			// rejection. Library wording can change; the structured
			// ErrorKind cannot without a behaviour change.
			rejected := collectAdditionalPropertiesRejections(err)
			if len(rejected) == 0 {
				t.Fatalf("workflow#540: no additionalProperties ErrorKind in error tree; got %v", err)
			}
			for _, want := range c.wantKeys {
				if !rejected[want] {
					t.Errorf("workflow#540: expected key %q rejected by additionalProperties; rejected=%v err=%v",
						want, keysOf(rejected), err)
				}
			}
		})
	}
}

// collectAdditionalPropertiesRejections walks a validation error
// (typically wrapped by ParseManifest) and returns every key that the
// jsonschema library flagged via the `additionalProperties` keyword.
// Returns an empty map if no such ErrorKind is present, which
// distinguishes a real workflow#540 regression from an unrelated
// failure (JSON parse, schema compile, etc.).
func collectAdditionalPropertiesRejections(err error) map[string]bool {
	out := map[string]bool{}
	var verr *jsonschema.ValidationError
	if !errors.As(err, &verr) {
		return out
	}
	var visit func(*jsonschema.ValidationError)
	visit = func(e *jsonschema.ValidationError) {
		if ap, ok := e.ErrorKind.(*kind.AdditionalProperties); ok {
			for _, p := range ap.Properties {
				out[p] = true
			}
		}
		for _, child := range e.Causes {
			visit(child)
		}
	}
	visit(verr)
	return out
}

func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out) // stable diagnostic output
	return out
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
