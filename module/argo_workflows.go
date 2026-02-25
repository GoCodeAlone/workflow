package module

import (
	"fmt"
	"time"

	"github.com/CrisisTextLine/modular"
)

// ArgoWorkflowState holds the current state of a managed Argo Workflows installation.
type ArgoWorkflowState struct {
	Name      string    `json:"name"`
	Namespace string    `json:"namespace"`
	Version   string    `json:"version"`
	Status    string    `json:"status"` // pending, creating, running, deleting, deleted
	Endpoint  string    `json:"endpoint"`
	CreatedAt time.Time `json:"createdAt"`
}

// ArgoWorkflowSpec is the translated Argo Workflow CRD spec derived from pipeline config.
type ArgoWorkflowSpec struct {
	APIVersion string            `json:"apiVersion"`
	Kind       string            `json:"kind"`
	Name       string            `json:"name"`
	Namespace  string            `json:"namespace"`
	Entrypoint string            `json:"entrypoint"`
	Templates  []ArgoTemplate    `json:"templates"`
	Arguments  map[string]string `json:"arguments,omitempty"`
}

// ArgoTemplate is a single template (DAG or step list) within an Argo Workflow.
type ArgoTemplate struct {
	Name string          `json:"name"`
	Kind string          `json:"kind"` // dag, steps, container
	DAG  []ArgoDAGTask   `json:"dag,omitempty"`
	Container *ArgoContainer `json:"container,omitempty"`
}

// ArgoDAGTask is a task node in a DAG template.
type ArgoDAGTask struct {
	Name         string   `json:"name"`
	Template     string   `json:"template"`
	Dependencies []string `json:"dependencies,omitempty"`
}

// ArgoContainer describes the container spec for a container template.
type ArgoContainer struct {
	Image   string            `json:"image"`
	Command []string          `json:"command,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

// ArgoWorkflowModule manages an Argo Workflows installation on Kubernetes.
// Config:
//
//	cluster:    name of a platform.kubernetes module (resolved from service registry)
//	namespace:  Kubernetes namespace for Argo (default: argo)
//	version:    Argo Workflows version (e.g. "3.5")
type ArgoWorkflowsModule struct {
	name    string
	config  map[string]any
	cluster *PlatformKubernetes // resolved from service registry (optional)
	state   *ArgoWorkflowState
	backend argoBackend
}

// argoBackend is the internal interface for Argo Workflows backends.
type argoBackend interface {
	plan(m *ArgoWorkflowsModule) (*PlatformPlan, error)
	apply(m *ArgoWorkflowsModule) (*PlatformResult, error)
	status(m *ArgoWorkflowsModule) (*ArgoWorkflowState, error)
	destroy(m *ArgoWorkflowsModule) error
	submitWorkflow(m *ArgoWorkflowsModule, spec *ArgoWorkflowSpec) (string, error)
	workflowStatus(m *ArgoWorkflowsModule, workflowName string) (string, error)
	workflowLogs(m *ArgoWorkflowsModule, workflowName string) ([]string, error)
	deleteWorkflow(m *ArgoWorkflowsModule, workflowName string) error
	listWorkflows(m *ArgoWorkflowsModule, labelSelector string) ([]string, error)
}

// NewArgoWorkflowsModule creates a new ArgoWorkflowsModule.
func NewArgoWorkflowsModule(name string, cfg map[string]any) *ArgoWorkflowsModule {
	return &ArgoWorkflowsModule{name: name, config: cfg}
}

// Name returns the module name.
func (m *ArgoWorkflowsModule) Name() string { return m.name }

// Init resolves optional cluster reference and initialises the mock backend.
func (m *ArgoWorkflowsModule) Init(app modular.Application) error {
	clusterName, _ := m.config["cluster"].(string)
	if clusterName != "" {
		svc, ok := app.SvcRegistry()[clusterName]
		if !ok {
			return fmt.Errorf("argo.workflows %q: cluster service %q not found", m.name, clusterName)
		}
		k, ok := svc.(*PlatformKubernetes)
		if !ok {
			return fmt.Errorf("argo.workflows %q: service %q is not a *PlatformKubernetes (got %T)", m.name, clusterName, svc)
		}
		m.cluster = k
	}

	ns, _ := m.config["namespace"].(string)
	if ns == "" {
		ns = "argo"
	}
	version, _ := m.config["version"].(string)

	m.state = &ArgoWorkflowState{
		Name:      m.name,
		Namespace: ns,
		Version:   version,
		Status:    "pending",
	}

	m.backend = &argoMockBackend{}

	return app.RegisterService(m.name, m)
}

// ProvidesServices declares the service this module provides.
func (m *ArgoWorkflowsModule) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{Name: m.name, Description: "Argo Workflows: " + m.name, Instance: m},
	}
}

// RequiresServices returns nil — cluster is resolved by name, not declared.
func (m *ArgoWorkflowsModule) RequiresServices() []modular.ServiceDependency {
	return nil
}

// Plan returns the changes needed to install Argo Workflows.
func (m *ArgoWorkflowsModule) Plan() (*PlatformPlan, error) {
	return m.backend.plan(m)
}

// Apply installs or updates Argo Workflows.
func (m *ArgoWorkflowsModule) Apply() (*PlatformResult, error) {
	return m.backend.apply(m)
}

// Status returns the current Argo Workflows installation state.
func (m *ArgoWorkflowsModule) Status() (any, error) {
	return m.backend.status(m)
}

// Destroy uninstalls Argo Workflows.
func (m *ArgoWorkflowsModule) Destroy() error {
	return m.backend.destroy(m)
}

// SubmitWorkflow translates a pipeline config into an Argo Workflow spec and submits it.
// Returns the workflow run name.
func (m *ArgoWorkflowsModule) SubmitWorkflow(spec *ArgoWorkflowSpec) (string, error) {
	return m.backend.submitWorkflow(m, spec)
}

// WorkflowStatus returns the execution status of a workflow run.
func (m *ArgoWorkflowsModule) WorkflowStatus(workflowName string) (string, error) {
	return m.backend.workflowStatus(m, workflowName)
}

// WorkflowLogs returns log lines from a workflow run.
func (m *ArgoWorkflowsModule) WorkflowLogs(workflowName string) ([]string, error) {
	return m.backend.workflowLogs(m, workflowName)
}

// DeleteWorkflow removes a completed or failed workflow.
func (m *ArgoWorkflowsModule) DeleteWorkflow(workflowName string) error {
	return m.backend.deleteWorkflow(m, workflowName)
}

// ListWorkflows lists workflows matching the optional label selector.
func (m *ArgoWorkflowsModule) ListWorkflows(labelSelector string) ([]string, error) {
	return m.backend.listWorkflows(m, labelSelector)
}

// namespace returns the configured namespace, falling back to "argo".
func (m *ArgoWorkflowsModule) namespace() string {
	if m.state != nil && m.state.Namespace != "" {
		return m.state.Namespace
	}
	return "argo"
}

// TranslatePipelineToArgo converts a list of pipeline step configs into an ArgoWorkflowSpec
// using a DAG template where each step becomes a task with sequential dependencies.
func TranslatePipelineToArgo(workflowName, namespace string, steps []map[string]any) *ArgoWorkflowSpec {
	spec := &ArgoWorkflowSpec{
		APIVersion: "argoproj.io/v1alpha1",
		Kind:       "Workflow",
		Name:       workflowName,
		Namespace:  namespace,
		Entrypoint: "pipeline-dag",
		Templates:  []ArgoTemplate{},
	}

	dagTemplate := ArgoTemplate{
		Name: "pipeline-dag",
		Kind: "dag",
		DAG:  []ArgoDAGTask{},
	}

	var prevTaskName string
	for _, step := range steps {
		stepName, _ := step["name"].(string)
		if stepName == "" {
			continue
		}
		image, _ := step["image"].(string)
		if image == "" {
			image = "alpine:latest"
		}
		command, _ := step["command"].([]string)

		// Create container template for this step.
		containerTpl := ArgoTemplate{
			Name: stepName + "-tpl",
			Kind: "container",
			Container: &ArgoContainer{
				Image:   image,
				Command: command,
			},
		}
		spec.Templates = append(spec.Templates, containerTpl)

		// Add DAG task with sequential dependency on previous step.
		task := ArgoDAGTask{
			Name:     stepName,
			Template: stepName + "-tpl",
		}
		if prevTaskName != "" {
			task.Dependencies = []string{prevTaskName}
		}
		dagTemplate.DAG = append(dagTemplate.DAG, task)
		prevTaskName = stepName
	}

	spec.Templates = append([]ArgoTemplate{dagTemplate}, spec.Templates...)
	return spec
}

// ─── mock backend ─────────────────────────────────────────────────────────────

// argoMockBackend implements argoBackend using in-memory state.
// Real implementation would use the Argo Workflows REST API or Kubernetes client.
type argoMockBackend struct {
	workflows map[string]string   // name -> status
	logs      map[string][]string // name -> log lines
}

func (b *argoMockBackend) ensureInit() {
	if b.workflows == nil {
		b.workflows = make(map[string]string)
	}
	if b.logs == nil {
		b.logs = make(map[string][]string)
	}
}

func (b *argoMockBackend) plan(m *ArgoWorkflowsModule) (*PlatformPlan, error) {
	plan := &PlatformPlan{
		Provider: "argo.workflows",
		Resource: m.name,
	}
	switch m.state.Status {
	case "pending", "deleted":
		plan.Actions = []PlatformAction{
			{Type: "create", Resource: m.name, Detail: fmt.Sprintf("install Argo Workflows in namespace %q", m.namespace())},
		}
	case "running":
		plan.Actions = []PlatformAction{
			{Type: "noop", Resource: m.name, Detail: "Argo Workflows already running"},
		}
	default:
		plan.Actions = []PlatformAction{
			{Type: "noop", Resource: m.name, Detail: fmt.Sprintf("status=%s, no action", m.state.Status)},
		}
	}
	return plan, nil
}

func (b *argoMockBackend) apply(m *ArgoWorkflowsModule) (*PlatformResult, error) {
	if m.state.Status == "running" {
		return &PlatformResult{Success: true, Message: "Argo Workflows already running", State: m.state}, nil
	}
	m.state.Status = "creating"
	m.state.CreatedAt = time.Now()
	// In-memory: immediately mark running.
	// Real: apply argo-workflows CRDs + deployment via kubectl or Helm.
	m.state.Status = "running"
	m.state.Endpoint = fmt.Sprintf("http://argo-server.%s.svc.cluster.local:2746", m.namespace())
	b.ensureInit()
	return &PlatformResult{
		Success: true,
		Message: fmt.Sprintf("Argo Workflows %q installed in namespace %q (in-memory mock)", m.name, m.namespace()),
		State:   m.state,
	}, nil
}

func (b *argoMockBackend) status(m *ArgoWorkflowsModule) (*ArgoWorkflowState, error) {
	return m.state, nil
}

func (b *argoMockBackend) destroy(m *ArgoWorkflowsModule) error {
	if m.state.Status == "deleted" {
		return nil
	}
	m.state.Status = "deleted"
	m.state.Endpoint = ""
	return nil
}

func (b *argoMockBackend) submitWorkflow(m *ArgoWorkflowsModule, spec *ArgoWorkflowSpec) (string, error) {
	b.ensureInit()
	if m.state.Status != "running" {
		return "", fmt.Errorf("argo.workflows %q: not running (status=%s)", m.name, m.state.Status)
	}
	runName := fmt.Sprintf("%s-%d", spec.Name, time.Now().UnixNano())
	b.workflows[runName] = "Running"
	b.logs[runName] = []string{
		fmt.Sprintf("workflow %q submitted to namespace %q", runName, spec.Namespace),
		fmt.Sprintf("entrypoint: %s", spec.Entrypoint),
	}
	return runName, nil
}

func (b *argoMockBackend) workflowStatus(m *ArgoWorkflowsModule, workflowName string) (string, error) {
	b.ensureInit()
	status, ok := b.workflows[workflowName]
	if !ok {
		return "", fmt.Errorf("argo.workflows %q: workflow %q not found", m.name, workflowName)
	}
	// Simulate progression: Running → Succeeded.
	if status == "Running" {
		b.workflows[workflowName] = "Succeeded"
		return "Succeeded", nil
	}
	return status, nil
}

func (b *argoMockBackend) workflowLogs(m *ArgoWorkflowsModule, workflowName string) ([]string, error) {
	b.ensureInit()
	if _, ok := b.workflows[workflowName]; !ok {
		return nil, fmt.Errorf("argo.workflows %q: workflow %q not found", m.name, workflowName)
	}
	lines := b.logs[workflowName]
	if lines == nil {
		return []string{}, nil
	}
	return lines, nil
}

func (b *argoMockBackend) deleteWorkflow(m *ArgoWorkflowsModule, workflowName string) error {
	b.ensureInit()
	if _, ok := b.workflows[workflowName]; !ok {
		return fmt.Errorf("argo.workflows %q: workflow %q not found", m.name, workflowName)
	}
	delete(b.workflows, workflowName)
	delete(b.logs, workflowName)
	return nil
}

func (b *argoMockBackend) listWorkflows(m *ArgoWorkflowsModule, labelSelector string) ([]string, error) {
	b.ensureInit()
	names := make([]string, 0, len(b.workflows))
	for name := range b.workflows {
		names = append(names, name)
	}
	return names, nil
}
