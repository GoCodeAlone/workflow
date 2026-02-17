package platform

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// StdTemplateRegistry is an in-memory implementation of the TemplateRegistry
// interface. It stores templates indexed by name and version, supports listing,
// and resolves templates by substituting parameters into capability declarations.
type StdTemplateRegistry struct {
	mu sync.RWMutex
	// templates maps "name" -> "version" -> *WorkflowTemplate
	templates map[string]map[string]*WorkflowTemplate
	resolver  *TemplateResolver
}

// NewStdTemplateRegistry creates a new in-memory template registry.
func NewStdTemplateRegistry() *StdTemplateRegistry {
	return &StdTemplateRegistry{
		templates: make(map[string]map[string]*WorkflowTemplate),
		resolver:  NewTemplateResolver(),
	}
}

// Register adds a template to the registry. It returns an error if a template
// with the same name and version already exists, or if the template is invalid.
func (r *StdTemplateRegistry) Register(_ context.Context, template *WorkflowTemplate) error {
	if template == nil {
		return fmt.Errorf("template is nil")
	}
	if template.Name == "" {
		return fmt.Errorf("template name is required")
	}
	if template.Version == "" {
		return fmt.Errorf("template version is required")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	versions, ok := r.templates[template.Name]
	if !ok {
		versions = make(map[string]*WorkflowTemplate)
		r.templates[template.Name] = versions
	}

	if _, exists := versions[template.Version]; exists {
		return fmt.Errorf("template %q version %q already exists", template.Name, template.Version)
	}

	versions[template.Version] = template
	return nil
}

// Get retrieves a template by name and version. If version is empty, it
// returns the latest version (determined by semver-like string sorting).
func (r *StdTemplateRegistry) Get(_ context.Context, name, version string) (*WorkflowTemplate, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	versions, ok := r.templates[name]
	if !ok {
		return nil, fmt.Errorf("template %q not found", name)
	}

	if version == "" {
		return r.latestVersionLocked(name, versions)
	}

	tmpl, ok := versions[version]
	if !ok {
		return nil, fmt.Errorf("template %q version %q not found", name, version)
	}
	return tmpl, nil
}

// GetLatest retrieves the latest version of a template by name.
func (r *StdTemplateRegistry) GetLatest(_ context.Context, name string) (*WorkflowTemplate, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	versions, ok := r.templates[name]
	if !ok {
		return nil, fmt.Errorf("template %q not found", name)
	}

	return r.latestVersionLocked(name, versions)
}

// List returns summaries of all registered templates.
func (r *StdTemplateRegistry) List(_ context.Context) ([]*WorkflowTemplateSummary, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var summaries []*WorkflowTemplateSummary
	for _, versions := range r.templates {
		for _, tmpl := range versions {
			paramNames := make([]string, len(tmpl.Parameters))
			for i, p := range tmpl.Parameters {
				paramNames[i] = p.Name
			}
			summaries = append(summaries, &WorkflowTemplateSummary{
				Name:        tmpl.Name,
				Version:     tmpl.Version,
				Description: tmpl.Description,
				Parameters:  paramNames,
			})
		}
	}

	// Sort for deterministic output.
	sort.Slice(summaries, func(i, j int) bool {
		if summaries[i].Name != summaries[j].Name {
			return summaries[i].Name < summaries[j].Name
		}
		return compareVersions(summaries[i].Version, summaries[j].Version) < 0
	})

	return summaries, nil
}

// Resolve instantiates a template with the given parameters, producing
// concrete CapabilityDeclarations. If version is empty, the latest version is used.
func (r *StdTemplateRegistry) Resolve(_ context.Context, name, version string, params map[string]any) ([]CapabilityDeclaration, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	versions, ok := r.templates[name]
	if !ok {
		return nil, fmt.Errorf("template %q not found", name)
	}

	var tmpl *WorkflowTemplate
	var err error
	if version == "" {
		tmpl, err = r.latestVersionLocked(name, versions)
	} else {
		t, found := versions[version]
		if !found {
			err = fmt.Errorf("template %q version %q not found", name, version)
		}
		tmpl = t
	}
	if err != nil {
		return nil, err
	}

	return r.resolver.Resolve(tmpl, params)
}

// latestVersionLocked returns the template with the highest version string.
// Must be called with at least a read lock held.
func (r *StdTemplateRegistry) latestVersionLocked(name string, versions map[string]*WorkflowTemplate) (*WorkflowTemplate, error) {
	if len(versions) == 0 {
		return nil, fmt.Errorf("template %q has no versions", name)
	}

	var versionStrs []string
	for v := range versions {
		versionStrs = append(versionStrs, v)
	}

	sort.Slice(versionStrs, func(i, j int) bool {
		return compareVersions(versionStrs[i], versionStrs[j]) < 0
	})

	latest := versionStrs[len(versionStrs)-1]
	return versions[latest], nil
}

// compareVersions compares two semver-like version strings.
// Returns negative if a < b, 0 if equal, positive if a > b.
func compareVersions(a, b string) int {
	aParts := strings.Split(a, ".")
	bParts := strings.Split(b, ".")

	maxLen := len(aParts)
	if len(bParts) > maxLen {
		maxLen = len(bParts)
	}

	for i := 0; i < maxLen; i++ {
		var aNum, bNum int
		if i < len(aParts) {
			_, _ = fmt.Sscanf(aParts[i], "%d", &aNum)
		}
		if i < len(bParts) {
			_, _ = fmt.Sscanf(bParts[i], "%d", &bNum)
		}
		if aNum != bNum {
			return aNum - bNum
		}
	}
	return 0
}
