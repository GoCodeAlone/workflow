package module

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
	Name      string         `json:"name"`
	Kind      string         `json:"kind"` // dag, steps, container
	DAG       []ArgoDAGTask  `json:"dag,omitempty"`
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

// Init resolves optional cluster reference and initialises the backend.
// Config options:
//
//	backend:   mock (default) | real
//	endpoint:  Argo Server URL, e.g. http://localhost:2746 (required for backend: real)
//	token:     Bearer token for Argo Server auth (optional)
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

	backendType, _ := m.config["backend"].(string)
	if backendType == "" {
		backendType = "mock"
	}

	switch backendType {
	case "mock":
		m.backend = &argoMockBackend{}
	case "real":
		endpoint, _ := m.config["endpoint"].(string)
		if endpoint == "" {
			return fmt.Errorf("argo.workflows %q: 'endpoint' is required for backend=real", m.name)
		}
		token, _ := m.config["token"].(string)
		m.backend = &argoRealBackend{
			endpoint:   endpoint,
			token:      token,
			httpClient: &http.Client{Timeout: 30 * time.Second},
		}
		m.state.Endpoint = endpoint
	default:
		return fmt.Errorf("argo.workflows %q: unsupported backend %q (use mock or real)", m.name, backendType)
	}

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

// ─── real backend ─────────────────────────────────────────────────────────────

// argoRealBackend implements argoBackend using the Argo Workflows REST API.
// It targets the Argo Server HTTP API (default port 2746).
type argoRealBackend struct {
	endpoint   string // e.g. http://argo-server.argo.svc.cluster.local:2746
	token      string // Bearer token (optional)
	httpClient *http.Client
}

// doRequest performs an authenticated HTTP request against the Argo Server.
func (b *argoRealBackend) doRequest(ctx context.Context, method, path string, body any) ([]byte, int, error) {
	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("argo marshal request: %w", err)
		}
		reqBody = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, b.endpoint+path, reqBody)
	if err != nil {
		return nil, 0, fmt.Errorf("argo new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if b.token != "" {
		req.Header.Set("Authorization", "Bearer "+b.token)
	}

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("argo request %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	respData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("argo read response: %w", err)
	}
	return respData, resp.StatusCode, nil
}

func (b *argoRealBackend) plan(m *ArgoWorkflowsModule) (*PlatformPlan, error) {
	// Check if Argo Server is reachable by calling the version endpoint.
	_, status, connErr := b.doRequest(context.Background(), http.MethodGet, "/api/v1/version", nil)
	plan := &PlatformPlan{Provider: "argo.workflows", Resource: m.name}
	if connErr != nil || status != http.StatusOK {
		plan.Actions = []PlatformAction{
			{Type: "create", Resource: m.name, Detail: fmt.Sprintf("install/connect Argo Workflows at %s (namespace: %s)", b.endpoint, m.namespace())},
		}
		return plan, nil //nolint:nilerr // graceful fallback — unreachable server produces a plan action
	}
	plan.Actions = []PlatformAction{
		{Type: "noop", Resource: m.name, Detail: fmt.Sprintf("Argo Server reachable at %s", b.endpoint)},
	}
	return plan, nil
}

func (b *argoRealBackend) apply(m *ArgoWorkflowsModule) (*PlatformResult, error) {
	// Verify connectivity to Argo Server.
	data, status, err := b.doRequest(context.Background(), http.MethodGet, "/api/v1/version", nil)
	if err != nil {
		return nil, fmt.Errorf("argo.workflows %q: cannot reach server at %s: %w", m.name, b.endpoint, err)
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("argo.workflows %q: server returned status %d", m.name, status)
	}

	var versionResp struct {
		Version string `json:"version"`
	}
	_ = json.Unmarshal(data, &versionResp)
	if versionResp.Version != "" {
		m.state.Version = versionResp.Version
	}

	m.state.Status = "running"
	m.state.Endpoint = b.endpoint
	m.state.CreatedAt = time.Now()

	return &PlatformResult{
		Success: true,
		Message: fmt.Sprintf("Argo Server reachable at %s (version: %s)", b.endpoint, m.state.Version),
		State:   m.state,
	}, nil
}

func (b *argoRealBackend) status(m *ArgoWorkflowsModule) (*ArgoWorkflowState, error) {
	_, statusCode, connErr := b.doRequest(context.Background(), http.MethodGet, "/api/v1/version", nil)
	if connErr != nil || statusCode != http.StatusOK {
		m.state.Status = "error"
		return m.state, nil //nolint:nilerr // error status is reported in state, not as error
	}
	m.state.Status = "running"
	return m.state, nil
}

func (b *argoRealBackend) destroy(m *ArgoWorkflowsModule) error {
	// Destroy does not uninstall Argo — it simply marks the module as no longer managed.
	m.state.Status = "deleted"
	m.state.Endpoint = ""
	return nil
}

// submitWorkflow submits an Argo Workflow via the REST API.
// Returns the server-assigned workflow name.
func (b *argoRealBackend) submitWorkflow(m *ArgoWorkflowsModule, spec *ArgoWorkflowSpec) (string, error) {
	ns := m.namespace()
	if spec.Namespace != "" {
		ns = spec.Namespace
	}

	// Build the Argo Workflow CRD as a map to POST to the API.
	wf := argoWorkflowCRD(spec)
	reqBody := map[string]any{
		"namespace": ns,
		"workflow":  wf,
	}

	data, status, err := b.doRequest(context.Background(), http.MethodPost,
		fmt.Sprintf("/api/v1/workflows/%s", ns), reqBody)
	if err != nil {
		return "", fmt.Errorf("argo submit workflow: %w", err)
	}
	if status != http.StatusOK && status != http.StatusCreated {
		return "", fmt.Errorf("argo submit workflow: server returned %d: %s", status, string(data))
	}

	var result struct {
		Metadata struct {
			Name string `json:"name"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("argo submit workflow: parse response: %w", err)
	}
	return result.Metadata.Name, nil
}

func (b *argoRealBackend) workflowStatus(m *ArgoWorkflowsModule, workflowName string) (string, error) {
	ns := m.namespace()
	data, status, err := b.doRequest(context.Background(), http.MethodGet,
		fmt.Sprintf("/api/v1/workflows/%s/%s", ns, workflowName), nil)
	if err != nil {
		return "", fmt.Errorf("argo get workflow status: %w", err)
	}
	if status == http.StatusNotFound {
		return "", fmt.Errorf("argo.workflows: workflow %q not found", workflowName)
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("argo get workflow status: server returned %d", status)
	}

	var result struct {
		Status struct {
			Phase string `json:"phase"`
		} `json:"status"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("argo get workflow status: parse response: %w", err)
	}
	return result.Status.Phase, nil
}

func (b *argoRealBackend) workflowLogs(m *ArgoWorkflowsModule, workflowName string) ([]string, error) {
	ns := m.namespace()
	// Use the Argo log endpoint: GET /api/v1/workflows/{ns}/{name}/log?logOptions.container=main
	data, status, err := b.doRequest(context.Background(), http.MethodGet,
		fmt.Sprintf("/api/v1/workflows/%s/%s/log?logOptions.container=main&grep=&selector=", ns, workflowName), nil)
	if err != nil {
		return nil, fmt.Errorf("argo get workflow logs: %w", err)
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("argo get workflow logs: server returned %d: %s", status, string(data))
	}

	// The log endpoint returns newline-delimited JSON objects.
	var lines []string
	for _, rawLine := range bytes.Split(data, []byte("\n")) {
		rawLine = bytes.TrimSpace(rawLine)
		if len(rawLine) == 0 {
			continue
		}
		var entry struct {
			Result struct {
				Content string `json:"content"`
			} `json:"result"`
		}
		if err := json.Unmarshal(rawLine, &entry); err == nil && entry.Result.Content != "" {
			lines = append(lines, entry.Result.Content)
		} else {
			lines = append(lines, string(rawLine))
		}
	}
	return lines, nil
}

func (b *argoRealBackend) deleteWorkflow(m *ArgoWorkflowsModule, workflowName string) error {
	ns := m.namespace()
	data, status, err := b.doRequest(context.Background(), http.MethodDelete,
		fmt.Sprintf("/api/v1/workflows/%s/%s", ns, workflowName), nil)
	if err != nil {
		return fmt.Errorf("argo delete workflow: %w", err)
	}
	if status == http.StatusNotFound {
		return fmt.Errorf("argo.workflows: workflow %q not found", workflowName)
	}
	if status != http.StatusOK {
		return fmt.Errorf("argo delete workflow: server returned %d: %s", status, string(data))
	}
	return nil
}

func (b *argoRealBackend) listWorkflows(m *ArgoWorkflowsModule, labelSelector string) ([]string, error) {
	ns := m.namespace()
	path := fmt.Sprintf("/api/v1/workflows/%s", ns)
	if labelSelector != "" {
		path += "?listOptions.labelSelector=" + labelSelector
	}

	data, status, err := b.doRequest(context.Background(), http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("argo list workflows: %w", err)
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("argo list workflows: server returned %d: %s", status, string(data))
	}

	var result struct {
		Items []struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
		} `json:"items"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("argo list workflows: parse response: %w", err)
	}

	names := make([]string, 0, len(result.Items))
	for _, item := range result.Items {
		names = append(names, item.Metadata.Name)
	}
	return names, nil
}

// argoWorkflowCRD converts an ArgoWorkflowSpec into the map structure expected
// by the Argo Server REST API (mirrors the Workflow CRD structure).
func argoWorkflowCRD(spec *ArgoWorkflowSpec) map[string]any {
	templates := make([]map[string]any, 0, len(spec.Templates))
	for _, t := range spec.Templates {
		tmap := map[string]any{"name": t.Name}
		switch t.Kind {
		case "dag":
			tasks := make([]map[string]any, 0, len(t.DAG))
			for _, task := range t.DAG {
				tm := map[string]any{
					"name":     task.Name,
					"template": task.Template,
				}
				if len(task.Dependencies) > 0 {
					tm["dependencies"] = task.Dependencies
				}
				tasks = append(tasks, tm)
			}
			tmap["dag"] = map[string]any{"tasks": tasks}
		case "container":
			if t.Container != nil {
				c := map[string]any{
					"image": t.Container.Image,
				}
				if len(t.Container.Command) > 0 {
					c["command"] = t.Container.Command
				}
				if len(t.Container.Env) > 0 {
					envList := make([]map[string]any, 0, len(t.Container.Env))
					for k, v := range t.Container.Env {
						envList = append(envList, map[string]any{"name": k, "value": v})
					}
					c["env"] = envList
				}
				tmap["container"] = c
			}
		}
		templates = append(templates, tmap)
	}

	wf := map[string]any{
		"apiVersion": "argoproj.io/v1alpha1",
		"kind":       "Workflow",
		"metadata": map[string]any{
			"generateName": spec.Name + "-",
			"namespace":    spec.Namespace,
		},
		"spec": map[string]any{
			"entrypoint": spec.Entrypoint,
			"templates":  templates,
		},
	}

	if len(spec.Arguments) > 0 {
		params := make([]map[string]any, 0, len(spec.Arguments))
		for k, v := range spec.Arguments {
			params = append(params, map[string]any{"name": k, "value": v})
		}
		wf["spec"].(map[string]any)["arguments"] = map[string]any{"parameters": params}
	}

	return wf
}
