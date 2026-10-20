package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/dlclark/regexp2"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

// The unchanged official schema uses ECMA negative lookahead, which Go's
// standard regexp engine cannot represent. Both versions below already occur
// in the parent repository's dependency checksums; runtime smoke is stdlib only.
type ecmaRegexp struct{ expression *regexp2.Regexp }

func (r ecmaRegexp) MatchString(s string) bool {
	matched, err := r.expression.MatchString(s)
	return err == nil && matched
}
func (r ecmaRegexp) String() string { return r.expression.String() }
func compileECMA(pattern string) (jsonschema.Regexp, error) {
	re, err := regexp2.Compile(pattern, regexp2.ECMAScript)
	if err != nil {
		return nil, err
	}
	re.MatchTimeout = time.Second
	return ecmaRegexp{re}, nil
}

func TestOfficialManifestSchemas(t *testing.T) {
	for _, name := range []string{"plugin", "mcp"} {
		t.Run(name, func(t *testing.T) {
			root := packageRoot(t)
			path := filepath.Join(root, "tests", "schemas", name+".schema.json")
			compiler := jsonschema.NewCompiler()
			compiler.UseRegexpEngine(compileECMA)
			// Compile also checks the schema itself against its Draft2020-12 metaschema.
			schema, err := compiler.Compile(path)
			if err != nil {
				t.Fatal(err)
			}
			manifest := readObject(t, filepath.Join(root, name+".json"))
			if err = schema.Validate(manifest); err != nil {
				t.Fatal(err)
			}
			manifest["unexpected_property"] = true
			if schema.Validate(manifest) == nil {
				t.Fatal("official schema accepted an unknown property")
			}
			delete(manifest, "unexpected_property")
			if name == "plugin" {
				for _, invalid := range []string{"workflow--assistant", "workflow..assistant"} {
					manifest["name"] = invalid
					if schema.Validate(manifest) == nil {
						t.Fatal("official ECMA negative lookahead was not enforced")
					}
				}
			}
		})
	}
}
