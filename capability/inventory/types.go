// Package inventory builds generated capability inventories for Workflow
// plugins, registries, and applications.
package inventory

// Inventory is the top-level ecosystem capability report.
type Inventory struct {
	Metadata     Metadata     `json:"metadata"`
	Capabilities []Capability `json:"capabilities"`
	Findings     []Finding    `json:"findings,omitempty"`
}

// Metadata describes how an inventory artifact was generated.
type Metadata struct {
	Generator       string         `json:"generator"`
	GeneratedAt     string         `json:"generatedAt,omitempty"`
	WorkflowVersion string         `json:"workflowVersion,omitempty"`
	TaxonomyVersion string         `json:"taxonomyVersion,omitempty"`
	TaxonomyDigest  string         `json:"taxonomyDigest,omitempty"`
	RegistrySource  string         `json:"registrySource,omitempty"`
	LocalRepoRoot   string         `json:"localRepoRoot,omitempty"`
	Counts          map[string]int `json:"counts,omitempty"`
}

// Capability is a product-level capability row.
type Capability struct {
	ID          string     `json:"id"`
	Category    string     `json:"category"`
	Name        string     `json:"name"`
	Description string     `json:"description,omitempty"`
	Lifecycle   string     `json:"lifecycle,omitempty"`
	Tags        []string   `json:"tags,omitempty"`
	Providers   []Provider `json:"providers,omitempty"`
	Evidence    []Evidence `json:"evidence,omitempty"`
	Findings    []Finding  `json:"findings,omitempty"`
}

// Provider describes a plugin, package, or provider that supplies a capability.
type Provider struct {
	Name          string   `json:"name"`
	Kind          string   `json:"kind"`
	Version       string   `json:"version,omitempty"`
	ReleaseStatus string   `json:"releaseStatus,omitempty"`
	Source        string   `json:"source,omitempty"`
	Capabilities  []string `json:"capabilities,omitempty"`
}

// AppProfile is the top-level application capability report.
type AppProfile struct {
	Metadata Metadata  `json:"metadata"`
	Usage    []Usage   `json:"usage"`
	Findings []Finding `json:"findings,omitempty"`
}

// Usage describes a capability used by an application.
type Usage struct {
	CapabilityID string     `json:"capabilityId"`
	Category     string     `json:"category,omitempty"`
	Name         string     `json:"name,omitempty"`
	Mode         string     `json:"mode"`
	Confidence   string     `json:"confidence,omitempty"`
	Evidence     []Evidence `json:"evidence,omitempty"`
	Findings     []Finding  `json:"findings,omitempty"`
}

// Evidence points to the source that supports a capability or usage row.
type Evidence struct {
	SourceKind string `json:"sourceKind"`
	SourcePath string `json:"sourcePath,omitempty"`
	JSONPath   string `json:"jsonPath,omitempty"`
	Detail     string `json:"detail,omitempty"`
}

// Finding is a warning or error produced during inventory generation.
type Finding struct {
	Level        string     `json:"level"`
	Code         string     `json:"code"`
	Message      string     `json:"message"`
	CapabilityID string     `json:"capabilityId,omitempty"`
	Evidence     []Evidence `json:"evidence,omitempty"`
}
