// This file hosts the plugin SDK manifest schema and helpers used by wfctl to
// discover plugin capabilities.
//
// The SDK manifest is intentionally additive over [plugin.PluginManifest];
// it captures only the fields that wfctl validates before typed runtime
// capability discovery. After the strict lifecycle cutover, the typed
// CapabilitiesResponse.compute_plan_version declaration is authoritative and
// wfctl accepts "v2" only.
package sdk

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// manifestSchemaJSON is the JSON Schema validating the SDK manifest. It is
// embedded so wfctl always validates against the schema version compiled
// into the binary, not whatever happens to be on disk.
//
//go:embed manifest_schema.json
var manifestSchemaJSON []byte

// ManifestSchemaJSON returns the raw JSON Schema bytes used to validate
// SDK manifests. Exported for plugin authors and external tooling that
// want to validate plugin.json without depending on this package's
// ParseManifest entry point.
//
// Returns a copy so callers cannot mutate the embedded schema; the
// underlying slice from //go:embed is technically writable.
func ManifestSchemaJSON() []byte {
	return bytes.Clone(manifestSchemaJSON)
}

// Manifest captures the SDK-level fields wfctl reads from plugin.json.
// It is a strict subset of the full plugin.PluginManifest — only fields
// that gate apply-time dispatch live here.
type Manifest struct {
	// Name is the plugin name. Carried for diagnostics; the SDK schema
	// does not enforce shape (lowercase/hyphen rules live in plugin.PluginManifest).
	Name string `json:"name"`

	// IaCProvider holds IaC-provider-specific manifest fields. Empty
	// (zero-valued) when the plugin does not implement IaCProvider.
	IaCProvider IaCProvider `json:"iacProvider"`
}

// IaCProvider describes IaC-provider-specific manifest fields.
type IaCProvider struct {
	// ComputePlanVersion is parse-time manifest metadata retained for plugin
	// authors and validation tooling. Runtime selection is strict: the typed
	// CapabilitiesResponse.compute_plan_version gate accepts "v2" only and
	// routes through wfctlhelpers.ApplyPlanWithHooks.
	// Schema-validated against the enum ["v1","v2"]; "" passes validation for
	// older manifests, but load-time typed capability validation rejects non-v2
	// providers.
	ComputePlanVersion string `json:"computePlanVersion,omitempty"`
}

// EffectiveComputePlanVersion was removed per workflow#699 (2026-05-17):
// post-cutover "v1" is not a valid runtime value, so a default-to-v1
// accessor would lie. The manifest field is now a parse-time-validated
// advisory only — the authoritative gate is the typed
// CapabilitiesResponse.compute_plan_version check in
// cmd/wfctl/deploy_providers.go's discoverAndLoadIaCProvider, which
// rejects any plugin not declaring "v2" at load time.

// compiledSchema is the parsed manifest schema. It is compiled lazily on
// first ParseManifest call and cached for the process lifetime; the schema
// is embedded and immutable, so a single compilation is correct.
//
// The compilation is guarded by sync.Once so concurrent callers cannot race
// on the cache pointer or on the jsonschema compiler's internal state. Both
// the success result and the error are captured so subsequent calls return
// the same outcome without re-compiling.
var (
	compiledSchema     *jsonschema.Schema
	compiledSchemaErr  error
	compiledSchemaOnce sync.Once
)

// loadSchema compiles manifestSchemaJSON exactly once per process. Returns
// the compiled schema or an error wrapping the underlying compile failure
// (so failures surface with a clear "schema bug" diagnostic rather than as
// a generic "ParseManifest failed").
func loadSchema() (*jsonschema.Schema, error) {
	compiledSchemaOnce.Do(func() {
		doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(manifestSchemaJSON))
		if err != nil {
			compiledSchemaErr = fmt.Errorf("sdk manifest schema: unmarshal: %w", err)
			return
		}
		c := jsonschema.NewCompiler()
		if err := c.AddResource("manifest.json", doc); err != nil {
			compiledSchemaErr = fmt.Errorf("sdk manifest schema: add resource: %w", err)
			return
		}
		s, err := c.Compile("manifest.json")
		if err != nil {
			compiledSchemaErr = fmt.Errorf("sdk manifest schema: compile: %w", err)
			return
		}
		compiledSchema = s
	})
	return compiledSchema, compiledSchemaErr
}

// ParseManifest validates raw plugin.json bytes against the SDK schema and
// decodes them into a Manifest. Returns an error if the JSON is malformed
// or violates the schema (e.g., iacProvider.computePlanVersion not in
// {"v1","v2"}). Pure-additive: existing plugin.json files without an
// iacProvider key parse cleanly with a zero-valued IaCProvider.
func ParseManifest(data []byte) (*Manifest, error) {
	s, err := loadSchema()
	if err != nil {
		return nil, err
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("manifest: invalid JSON: %w", err)
	}
	if err := s.Validate(doc); err != nil {
		return nil, fmt.Errorf("manifest: schema validation failed: %w", err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("manifest: decode: %w", err)
	}
	return &m, nil
}
