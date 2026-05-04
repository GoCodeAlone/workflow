// Package sdk hosts the plugin SDK manifest schema and helpers used by
// wfctl to discover plugin capabilities (currently: IaC dispatch version).
//
// The SDK manifest is intentionally additive over [plugin.PluginManifest];
// it captures only the fields that wfctl reads at apply-time to choose
// between the v1 (legacy in-provider Apply) and v2 (wfctlhelpers.ApplyPlan)
// dispatch paths.
package sdk

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"

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
func ManifestSchemaJSON() []byte {
	return manifestSchemaJSON
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
	// ComputePlanVersion selects the apply-time dispatch path:
	//   "" (default, treated as "v1"): legacy in-provider Apply switch.
	//   "v1":                          explicit legacy dispatch.
	//   "v2":                          route through wfctlhelpers.ApplyPlan
	//                                   (Replace + input-drift postcondition).
	// Schema-validated against the enum ["v1","v2"]; "" passes validation
	// because the field is optional.
	ComputePlanVersion string `json:"computePlanVersion,omitempty"`
}

// EffectiveComputePlanVersion returns the dispatch version, defaulting to
// "v1" when the manifest omits the field. Callers should always go through
// this accessor rather than reading ComputePlanVersion directly so the
// default-v1 contract stays in one place.
func (p IaCProvider) EffectiveComputePlanVersion() string {
	if p.ComputePlanVersion == "" {
		return "v1"
	}
	return p.ComputePlanVersion
}

// compiledSchema is the parsed manifest schema. It is compiled lazily on
// first ParseManifest call and cached for the process lifetime; the schema
// is embedded and immutable, so a single compilation is correct.
var compiledSchema *jsonschema.Schema

// loadSchema compiles manifestSchemaJSON. Returns the compiled schema or
// an error wrapping the underlying compile failure. Separate function so
// failures surface with a clear "schema bug" diagnostic rather than as a
// generic "ParseManifest failed."
func loadSchema() (*jsonschema.Schema, error) {
	if compiledSchema != nil {
		return compiledSchema, nil
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(manifestSchemaJSON))
	if err != nil {
		return nil, fmt.Errorf("sdk manifest schema: unmarshal: %w", err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource("manifest.json", doc); err != nil {
		return nil, fmt.Errorf("sdk manifest schema: add resource: %w", err)
	}
	s, err := c.Compile("manifest.json")
	if err != nil {
		return nil, fmt.Errorf("sdk manifest schema: compile: %w", err)
	}
	compiledSchema = s
	return s, nil
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
