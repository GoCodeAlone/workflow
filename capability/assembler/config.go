package assembler

import (
	"strings"

	"github.com/GoCodeAlone/workflow/schema"
)

// defaultName derives a module instance name from its type (last segment):
// http.server -> "server" ; auth.jwt -> "jwt" ; database.workflow -> "workflow".
func defaultName(typ string) string {
	for i := len(typ) - 1; i >= 0; i-- {
		if typ[i] == '.' || typ[i] == ':' {
			return typ[i+1:]
		}
	}
	return typ
}

// envPrefix derives the module-type segment for env-var namespacing (P2b):
// auth.jwt -> JWT ; database.workflow -> WORKFLOW ; disambiguates multi-module scaffolds.
func envPrefix(moduleType string) string {
	return strings.ToUpper(snakeCase(defaultName(moduleType)))
}

// genConfig produces a starter config for moduleType from the registry:
// DefaultConfig as base; for every Required field lacking a default, emit a
// placeholder — namespaced ${<TYPESEG>_<FIELD>} when Sensitive (V4, P2b), else
// the schema Placeholder or a type-sensible default. ok=false if type ∉ registry
// (existence gate, D6).
func genConfig(moduleType string, reg *schema.ModuleSchemaRegistry) (map[string]any, bool) {
	s := reg.Get(moduleType)
	if s == nil {
		return nil, false
	}
	cfg := map[string]any{}
	for k, v := range s.DefaultConfig {
		cfg[k] = v
	}
	for _, f := range s.ConfigFields {
		if _, set := cfg[f.Key]; set {
			continue
		}
		if !f.Required {
			continue // optional + no default -> omit (clean scaffold)
		}
		cfg[f.Key] = placeholderFor(moduleType, f)
	}
	return cfg, true
}

// placeholderFor returns the starter value for a required field without a default.
// Sensitive -> namespaced env ref ${<TYPESEG>_<FIELD>} (P2b, e.g. auth.jwt/secret
// -> ${JWT_SECRET}); else Placeholder or a type-sensible default (select -> first
// Option, P2: database.workflow.driver).
func placeholderFor(moduleType string, f schema.ConfigFieldDef) any {
	if f.Sensitive {
		return "${" + envPrefix(moduleType) + "_" + strings.ToUpper(snakeCase(f.Key)) + "}"
	}
	if f.Placeholder != "" {
		return f.Placeholder
	}
	return zeroFor(f)
}

func zeroFor(f schema.ConfigFieldDef) any {
	switch f.Type {
	case schema.FieldTypeSelect:
		if len(f.Options) > 0 {
			return f.Options[0] // valid select default (P2)
		}
		return ""
	case schema.FieldTypeString, schema.FieldTypeDuration, schema.FieldTypeSQL, schema.FieldTypeFilePath:
		return ""
	case schema.FieldTypeNumber:
		return 0
	case schema.FieldTypeBool:
		return false
	default:
		return ""
	}
}

// snakeCase converts camelCase/json keys to UPPER_SNAKE for env-var refs.
// (e.g. tokenExpiry -> TOKEN_EXPIRY; dsn -> DSN; accessKeyId -> ACCESS_KEY_ID).
func snakeCase(s string) string {
	var b strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			b.WriteByte('_')
		}
		if r >= 'a' && r <= 'z' {
			r -= 'a' - 'A'
		}
		b.WriteRune(r)
	}
	return b.String()
}
