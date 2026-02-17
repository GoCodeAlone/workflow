package plugin

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// PluginWorkflowRegistry stores embedded workflows contributed by plugins.
// Workflows are keyed by qualified name: "plugin-name:workflow-name".
type PluginWorkflowRegistry struct {
	mu        sync.RWMutex
	workflows map[string]*EmbeddedWorkflow
}

// NewPluginWorkflowRegistry creates an empty PluginWorkflowRegistry.
func NewPluginWorkflowRegistry() *PluginWorkflowRegistry {
	return &PluginWorkflowRegistry{
		workflows: make(map[string]*EmbeddedWorkflow),
	}
}

// qualifiedName builds "pluginName:workflowName".
func qualifiedName(pluginName, workflowName string) string {
	return pluginName + ":" + workflowName
}

// Register adds an embedded workflow under the given plugin name.
func (r *PluginWorkflowRegistry) Register(pluginName string, wf EmbeddedWorkflow) error {
	if pluginName == "" {
		return fmt.Errorf("plugin name must not be empty")
	}
	if wf.Name == "" {
		return fmt.Errorf("workflow name must not be empty")
	}

	qn := qualifiedName(pluginName, wf.Name)

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.workflows[qn]; exists {
		return fmt.Errorf("workflow %q already registered", qn)
	}

	copy := wf // shallow copy so caller can't mutate registry state
	r.workflows[qn] = &copy
	return nil
}

// Get retrieves a workflow by its qualified name ("plugin:workflow").
func (r *PluginWorkflowRegistry) Get(qualifiedName string) (*EmbeddedWorkflow, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	wf, ok := r.workflows[qualifiedName]
	return wf, ok
}

// List returns all registered qualified workflow names, sorted.
func (r *PluginWorkflowRegistry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.workflows))
	for k := range r.workflows {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// Unregister removes all workflows belonging to the given plugin.
func (r *PluginWorkflowRegistry) Unregister(pluginName string) {
	prefix := pluginName + ":"

	r.mu.Lock()
	defer r.mu.Unlock()

	for k := range r.workflows {
		if strings.HasPrefix(k, prefix) {
			delete(r.workflows, k)
		}
	}
}
