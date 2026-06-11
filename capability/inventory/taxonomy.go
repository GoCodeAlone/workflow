package inventory

import (
	"crypto/sha256"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Taxonomy maps raw Workflow/plugin type declarations to product capabilities.
type Taxonomy struct {
	Version      string               `json:"version" yaml:"version"`
	Capabilities []TaxonomyCapability `json:"capabilities" yaml:"capabilities"`

	byID    map[string]*TaxonomyCapability
	aliases map[string]*TaxonomyCapability
	digest  string
}

// TaxonomyCapability is one product capability declared in the taxonomy file.
type TaxonomyCapability struct {
	ID          string          `json:"id" yaml:"id"`
	Category    string          `json:"category" yaml:"category"`
	Name        string          `json:"name" yaml:"name"`
	Description string          `json:"description,omitempty" yaml:"description,omitempty"`
	Lifecycle   string          `json:"lifecycle,omitempty" yaml:"lifecycle,omitempty"`
	Aliases     TaxonomyAliases `json:"aliases,omitempty" yaml:"aliases,omitempty"`
	Tags        []string        `json:"tags,omitempty" yaml:"tags,omitempty"`
}

// TaxonomyAliases lists raw declaration names that map to a capability.
type TaxonomyAliases struct {
	ModuleTypes             []string `json:"moduleTypes,omitempty" yaml:"moduleTypes,omitempty"`
	BuiltinModuleTypes      []string `json:"builtinModuleTypes,omitempty" yaml:"builtinModuleTypes,omitempty"`
	StepTypes               []string `json:"stepTypes,omitempty" yaml:"stepTypes,omitempty"`
	BuiltinStepTypes        []string `json:"builtinStepTypes,omitempty" yaml:"builtinStepTypes,omitempty"`
	TriggerTypes            []string `json:"triggerTypes,omitempty" yaml:"triggerTypes,omitempty"`
	BuiltinTriggerTypes     []string `json:"builtinTriggerTypes,omitempty" yaml:"builtinTriggerTypes,omitempty"`
	WorkflowTypes           []string `json:"workflowTypes,omitempty" yaml:"workflowTypes,omitempty"`
	BuiltinWorkflowTypes    []string `json:"builtinWorkflowTypes,omitempty" yaml:"builtinWorkflowTypes,omitempty"`
	WiringHooks             []string `json:"wiringHooks,omitempty" yaml:"wiringHooks,omitempty"`
	BuiltinWiringHooks      []string `json:"builtinWiringHooks,omitempty" yaml:"builtinWiringHooks,omitempty"`
	IaCServices             []string `json:"iacServices,omitempty" yaml:"iacServices,omitempty"`
	IaCStateBackends        []string `json:"iacStateBackends,omitempty" yaml:"iacStateBackends,omitempty"`
	BuiltinIaCStateBackends []string `json:"builtinIaCStateBackends,omitempty" yaml:"builtinIaCStateBackends,omitempty"`
	Providers               []string `json:"providers,omitempty" yaml:"providers,omitempty"`
	Plugins                 []string `json:"plugins,omitempty" yaml:"plugins,omitempty"`
	Keywords                []string `json:"keywords,omitempty" yaml:"keywords,omitempty"`
}

// LoadTaxonomy reads and validates a taxonomy YAML file.
func LoadTaxonomy(path string) (*Taxonomy, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read taxonomy: %w", err)
	}
	var tax Taxonomy
	if err := yaml.Unmarshal(data, &tax); err != nil {
		return nil, fmt.Errorf("parse taxonomy: %w", err)
	}
	sum := sha256.Sum256(data)
	tax.digest = fmt.Sprintf("%x", sum[:])
	if err := tax.index(); err != nil {
		return nil, err
	}
	return &tax, nil
}

// Digest returns the SHA-256 digest of the taxonomy source file.
func (t *Taxonomy) Digest() string {
	if t == nil {
		return ""
	}
	return t.digest
}

// MatchType returns the taxonomy capability for a raw type declaration.
func (t *Taxonomy) MatchType(kind, value string) (*TaxonomyCapability, bool) {
	if t == nil {
		return nil, false
	}
	cap, ok := t.aliases[aliasKey(kind, value)]
	return cap, ok
}

// ByID returns a capability by stable taxonomy ID.
func (t *Taxonomy) ByID(id string) (*TaxonomyCapability, bool) {
	if t == nil {
		return nil, false
	}
	cap, ok := t.byID[id]
	return cap, ok
}

func (t *Taxonomy) index() error {
	t.byID = make(map[string]*TaxonomyCapability, len(t.Capabilities))
	t.aliases = make(map[string]*TaxonomyCapability)
	for i := range t.Capabilities {
		cap := &t.Capabilities[i]
		if strings.TrimSpace(cap.ID) == "" {
			return fmt.Errorf("taxonomy: capability id is required")
		}
		if _, ok := t.byID[cap.ID]; ok {
			return fmt.Errorf("taxonomy: duplicate capability id %q", cap.ID)
		}
		t.byID[cap.ID] = cap
		for _, alias := range cap.aliasPairs() {
			key := aliasKey(alias.kind, alias.value)
			if existing, ok := t.aliases[key]; ok {
				return fmt.Errorf("taxonomy: duplicate alias %s=%q for %q and %q", alias.kind, alias.value, existing.ID, cap.ID)
			}
			t.aliases[key] = cap
		}
	}
	return nil
}

type taxonomyAlias struct {
	kind  string
	value string
}

func (c TaxonomyCapability) aliasPairs() []taxonomyAlias {
	var aliases []taxonomyAlias
	add := func(kind string, values []string) {
		for _, value := range values {
			if strings.TrimSpace(value) != "" {
				aliases = append(aliases, taxonomyAlias{kind: kind, value: value})
			}
		}
	}
	add("module", c.Aliases.ModuleTypes)
	add("module", c.Aliases.BuiltinModuleTypes)
	add("step", c.Aliases.StepTypes)
	add("step", c.Aliases.BuiltinStepTypes)
	add("trigger", c.Aliases.TriggerTypes)
	add("trigger", c.Aliases.BuiltinTriggerTypes)
	add("workflow", c.Aliases.WorkflowTypes)
	add("workflow", c.Aliases.BuiltinWorkflowTypes)
	add("wiringHook", c.Aliases.WiringHooks)
	add("wiringHook", c.Aliases.BuiltinWiringHooks)
	add("iacService", c.Aliases.IaCServices)
	add("iacStateBackend", c.Aliases.IaCStateBackends)
	add("iacStateBackend", c.Aliases.BuiltinIaCStateBackends)
	add("provider", c.Aliases.Providers)
	add("plugin", c.Aliases.Plugins)
	add("keyword", c.Aliases.Keywords)
	return aliases
}

func (c TaxonomyCapability) hasTag(tag string) bool {
	for _, candidate := range c.Tags {
		if strings.EqualFold(strings.TrimSpace(candidate), tag) {
			return true
		}
	}
	return false
}

func (c TaxonomyCapability) hasBuiltinAlias(kind, value string) bool {
	switch kind {
	case "module":
		return containsAlias(c.Aliases.BuiltinModuleTypes, value)
	case "step":
		return containsAlias(c.Aliases.BuiltinStepTypes, value)
	case "trigger":
		return containsAlias(c.Aliases.BuiltinTriggerTypes, value)
	case "workflow":
		return containsAlias(c.Aliases.BuiltinWorkflowTypes, value)
	case "wiringHook":
		return containsAlias(c.Aliases.BuiltinWiringHooks, value)
	case "iacStateBackend":
		return containsAlias(c.Aliases.BuiltinIaCStateBackends, value)
	default:
		return false
	}
}

func containsAlias(values []string, value string) bool {
	want := strings.ToLower(strings.TrimSpace(value))
	for _, candidate := range values {
		if strings.ToLower(strings.TrimSpace(candidate)) == want {
			return true
		}
	}
	return false
}

func aliasKey(kind, value string) string {
	return strings.ToLower(strings.TrimSpace(kind)) + ":" + strings.ToLower(strings.TrimSpace(value))
}
