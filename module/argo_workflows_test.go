package module_test

import (
	"context"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/module"
)

// ─── module lifecycle tests ───────────────────────────────────────────────────

func TestArgoWorkflows_InitAndStatus(t *testing.T) {
	app := module.NewMockApplication()
	m := module.NewArgoWorkflowsModule("argo-test", map[string]any{
		"namespace": "argo",
		"version":   "3.5",
	})
	if err := m.Init(app); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	svc, ok := app.Services["argo-test"]
	if !ok {
		t.Fatal("expected argo-test in service registry")
	}
	if _, ok := svc.(*module.ArgoWorkflowsModule); !ok {
		t.Fatalf("registry entry is %T, want *ArgoWorkflowsModule", svc)
	}

	st, err := m.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	state := st.(*module.ArgoWorkflowState)
	if state.Status != "pending" {
		t.Errorf("expected pending, got %q", state.Status)
	}
	if state.Namespace != "argo" {
		t.Errorf("expected namespace=argo, got %q", state.Namespace)
	}
}

func TestArgoWorkflows_PlanCreateOnPending(t *testing.T) {
	app := module.NewMockApplication()
	m := module.NewArgoWorkflowsModule("argo", map[string]any{"namespace": "argo"})
	if err := m.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}

	plan, err := m.Plan()
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plan.Actions) == 0 {
		t.Fatal("expected at least one action")
	}
	if plan.Actions[0].Type != "create" {
		t.Errorf("expected action=create, got %q", plan.Actions[0].Type)
	}
	if plan.Provider != "argo.workflows" {
		t.Errorf("expected provider=argo.workflows, got %q", plan.Provider)
	}
}

func TestArgoWorkflows_ApplyAndNoop(t *testing.T) {
	app := module.NewMockApplication()
	m := module.NewArgoWorkflowsModule("argo", map[string]any{"namespace": "argo"})
	if err := m.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}

	result, err := m.Apply()
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if !result.Success {
		t.Error("expected success=true")
	}

	result2, err := m.Apply()
	if err != nil {
		t.Fatalf("second Apply: %v", err)
	}
	if !result2.Success {
		t.Error("expected success=true on noop")
	}
	if !strings.Contains(result2.Message, "already running") {
		t.Errorf("expected 'already running' in message, got %q", result2.Message)
	}

	st, err := m.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	state := st.(*module.ArgoWorkflowState)
	if state.Status != "running" {
		t.Errorf("expected running, got %q", state.Status)
	}
	if state.Endpoint == "" {
		t.Error("expected non-empty endpoint after apply")
	}
}

func TestArgoWorkflows_PlanNoopAfterApply(t *testing.T) {
	app := module.NewMockApplication()
	m := module.NewArgoWorkflowsModule("argo", map[string]any{})
	if err := m.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if _, err := m.Apply(); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	plan, err := m.Plan()
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plan.Actions) == 0 || plan.Actions[0].Type != "noop" {
		t.Errorf("expected noop after apply, got %+v", plan.Actions)
	}
}

func TestArgoWorkflows_Destroy(t *testing.T) {
	app := module.NewMockApplication()
	m := module.NewArgoWorkflowsModule("argo", map[string]any{})
	if err := m.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if _, err := m.Apply(); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if err := m.Destroy(); err != nil {
		t.Fatalf("Destroy: %v", err)
	}
	st, err := m.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	state := st.(*module.ArgoWorkflowState)
	if state.Status != "deleted" {
		t.Errorf("expected deleted, got %q", state.Status)
	}
}

func TestArgoWorkflows_ClusterResolution(t *testing.T) {
	app := module.NewMockApplication()
	k := module.NewPlatformKubernetes("my-cluster", map[string]any{"type": "kind"})
	if err := k.Init(app); err != nil {
		t.Fatalf("k8s Init: %v", err)
	}
	m := module.NewArgoWorkflowsModule("argo", map[string]any{"cluster": "my-cluster"})
	if err := m.Init(app); err != nil {
		t.Fatalf("argo Init: %v", err)
	}
}

func TestArgoWorkflows_ClusterNotFound(t *testing.T) {
	app := module.NewMockApplication()
	m := module.NewArgoWorkflowsModule("argo", map[string]any{"cluster": "nonexistent"})
	if err := m.Init(app); err == nil {
		t.Error("expected error for missing cluster, got nil")
	}
}

func TestArgoWorkflows_ClusterWrongType(t *testing.T) {
	app := module.NewMockApplication()
	_ = app.RegisterService("not-a-cluster", "just-a-string")
	m := module.NewArgoWorkflowsModule("argo", map[string]any{"cluster": "not-a-cluster"})
	if err := m.Init(app); err == nil {
		t.Error("expected error for wrong type, got nil")
	}
}

// ─── DAG translation tests ────────────────────────────────────────────────────

func TestTranslatePipelineToArgo_DAGGeneration(t *testing.T) {
	steps := []map[string]any{
		{"name": "clone", "image": "alpine/git:latest", "command": []string{"git", "clone", "https://example.com/repo"}},
		{"name": "build", "image": "golang:1.22", "command": []string{"go", "build", "./..."}},
		{"name": "test", "image": "golang:1.22", "command": []string{"go", "test", "./..."}},
	}

	spec := module.TranslatePipelineToArgo("my-pipeline", "argo", steps)

	if spec.Kind != "Workflow" {
		t.Errorf("expected Kind=Workflow, got %q", spec.Kind)
	}
	if spec.APIVersion != "argoproj.io/v1alpha1" {
		t.Errorf("expected APIVersion=argoproj.io/v1alpha1, got %q", spec.APIVersion)
	}
	if spec.Entrypoint != "pipeline-dag" {
		t.Errorf("expected entrypoint=pipeline-dag, got %q", spec.Entrypoint)
	}
	if spec.Namespace != "argo" {
		t.Errorf("expected namespace=argo, got %q", spec.Namespace)
	}

	// dag template + 3 container templates
	if len(spec.Templates) != 4 {
		t.Fatalf("expected 4 templates, got %d", len(spec.Templates))
	}

	dag := spec.Templates[0]
	if dag.Kind != "dag" {
		t.Errorf("first template should be dag, got %q", dag.Kind)
	}
	if len(dag.DAG) != 3 {
		t.Fatalf("expected 3 DAG tasks, got %d", len(dag.DAG))
	}
	if len(dag.DAG[0].Dependencies) != 0 {
		t.Errorf("first task should have no dependencies")
	}
	if len(dag.DAG[1].Dependencies) != 1 || dag.DAG[1].Dependencies[0] != "clone" {
		t.Errorf("second task should depend on clone, got %v", dag.DAG[1].Dependencies)
	}
	if len(dag.DAG[2].Dependencies) != 1 || dag.DAG[2].Dependencies[0] != "build" {
		t.Errorf("third task should depend on build, got %v", dag.DAG[2].Dependencies)
	}
}

func TestTranslatePipelineToArgo_EmptySteps(t *testing.T) {
	spec := module.TranslatePipelineToArgo("empty", "argo", nil)
	if len(spec.Templates) != 1 {
		t.Errorf("expected 1 template (empty dag), got %d", len(spec.Templates))
	}
	if len(spec.Templates[0].DAG) != 0 {
		t.Errorf("expected empty dag, got %d tasks", len(spec.Templates[0].DAG))
	}
}

func TestTranslatePipelineToArgo_DefaultImage(t *testing.T) {
	steps := []map[string]any{{"name": "run"}}
	spec := module.TranslatePipelineToArgo("wf", "default", steps)
	if len(spec.Templates) < 2 {
		t.Fatal("expected at least 2 templates")
	}
	containerTpl := spec.Templates[1]
	if containerTpl.Container == nil {
		t.Fatal("expected container template")
	}
	if containerTpl.Container.Image != "alpine:latest" {
		t.Errorf("expected default image alpine:latest, got %q", containerTpl.Container.Image)
	}
}

// ─── submit/status/logs/delete lifecycle ──────────────────────────────────────

func newRunningArgoApp(t *testing.T) (*module.MockApplication, *module.ArgoWorkflowsModule) {
	t.Helper()
	app := module.NewMockApplication()
	m := module.NewArgoWorkflowsModule("argo-svc", map[string]any{"namespace": "argo"})
	if err := m.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if _, err := m.Apply(); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	return app, m
}

func TestArgoWorkflows_SubmitStatusLogsDeleteLifecycle(t *testing.T) {
	_, m := newRunningArgoApp(t)

	spec := module.TranslatePipelineToArgo("ci-run", "argo", []map[string]any{
		{"name": "build", "image": "golang:1.22"},
	})

	runName, err := m.SubmitWorkflow(spec)
	if err != nil {
		t.Fatalf("SubmitWorkflow: %v", err)
	}
	if runName == "" {
		t.Fatal("expected non-empty run name")
	}

	status, err := m.WorkflowStatus(runName)
	if err != nil {
		t.Fatalf("WorkflowStatus: %v", err)
	}
	if status != "Succeeded" {
		t.Errorf("expected Succeeded, got %q", status)
	}

	logs, err := m.WorkflowLogs(runName)
	if err != nil {
		t.Fatalf("WorkflowLogs: %v", err)
	}
	if len(logs) == 0 {
		t.Error("expected at least one log line")
	}

	if err := m.DeleteWorkflow(runName); err != nil {
		t.Fatalf("DeleteWorkflow: %v", err)
	}

	if _, err := m.WorkflowStatus(runName); err == nil {
		t.Error("expected error after deletion, got nil")
	}
}

func TestArgoWorkflows_SubmitRequiresRunning(t *testing.T) {
	app := module.NewMockApplication()
	m := module.NewArgoWorkflowsModule("argo", map[string]any{})
	if err := m.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}
	spec := module.TranslatePipelineToArgo("wf", "argo", nil)
	if _, err := m.SubmitWorkflow(spec); err == nil {
		t.Error("expected error submitting to non-running Argo, got nil")
	}
}

func TestArgoWorkflows_ListWorkflows(t *testing.T) {
	_, m := newRunningArgoApp(t)

	for _, wfName := range []string{"wf1", "wf2"} {
		spec := module.TranslatePipelineToArgo(wfName, "argo", nil)
		if _, err := m.SubmitWorkflow(spec); err != nil {
			t.Fatalf("SubmitWorkflow %s: %v", wfName, err)
		}
	}

	workflows, err := m.ListWorkflows("")
	if err != nil {
		t.Fatalf("ListWorkflows: %v", err)
	}
	if len(workflows) != 2 {
		t.Errorf("expected 2 workflows, got %d", len(workflows))
	}
}

func TestArgoWorkflows_DeleteNonexistent(t *testing.T) {
	_, m := newRunningArgoApp(t)
	if err := m.DeleteWorkflow("ghost-workflow"); err == nil {
		t.Error("expected error deleting nonexistent workflow, got nil")
	}
}

// ─── pipeline step tests ──────────────────────────────────────────────────────

func TestArgoSubmitStep(t *testing.T) {
	app, _ := newRunningArgoApp(t)

	factory := module.NewArgoSubmitStepFactory()
	step, err := factory("submit", map[string]any{
		"service":       "argo-svc",
		"workflow_name": "ci-pipeline",
		"steps": []any{
			map[string]any{"name": "build", "image": "golang:1.22"},
			map[string]any{"name": "test", "image": "golang:1.22"},
		},
	}, app)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}

	result, err := step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output["workflow_run"] == "" {
		t.Error("expected workflow_run in output")
	}
	if result.Output["service"] != "argo-svc" {
		t.Errorf("expected service=argo-svc, got %v", result.Output["service"])
	}
	// dag + build-tpl + test-tpl = 3
	if result.Output["templates"].(int) != 3 {
		t.Errorf("expected 3 templates, got %v", result.Output["templates"])
	}
}

func TestArgoSubmitStep_MissingService(t *testing.T) {
	factory := module.NewArgoSubmitStepFactory()
	_, err := factory("submit", map[string]any{}, module.NewMockApplication())
	if err == nil {
		t.Error("expected error for missing service, got nil")
	}
}

func TestArgoSubmitStep_ServiceNotFound(t *testing.T) {
	factory := module.NewArgoSubmitStepFactory()
	step, err := factory("submit", map[string]any{"service": "nonexistent"}, module.NewMockApplication())
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	_, err = step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err == nil {
		t.Error("expected error for missing service, got nil")
	}
}

func TestArgoStatusStep(t *testing.T) {
	app, m := newRunningArgoApp(t)

	spec := module.TranslatePipelineToArgo("ci", "argo", nil)
	runName, err := m.SubmitWorkflow(spec)
	if err != nil {
		t.Fatalf("SubmitWorkflow: %v", err)
	}

	factory := module.NewArgoStatusStepFactory()
	step, err := factory("status", map[string]any{
		"service":      "argo-svc",
		"workflow_run": runName,
	}, app)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}

	result, err := step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output["workflow_status"] != "Succeeded" {
		t.Errorf("expected Succeeded, got %v", result.Output["workflow_status"])
	}
	if result.Output["succeeded"] != true {
		t.Errorf("expected succeeded=true")
	}
}

func TestArgoStatusStep_FromContext(t *testing.T) {
	app, m := newRunningArgoApp(t)

	spec := module.TranslatePipelineToArgo("ci", "argo", nil)
	runName, err := m.SubmitWorkflow(spec)
	if err != nil {
		t.Fatalf("SubmitWorkflow: %v", err)
	}

	factory := module.NewArgoStatusStepFactory()
	step, err := factory("status", map[string]any{"service": "argo-svc"}, app)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	pc := &module.PipelineContext{Current: map[string]any{"workflow_run": runName}}
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output["workflow_run"] != runName {
		t.Errorf("expected workflow_run=%q, got %v", runName, result.Output["workflow_run"])
	}
}

func TestArgoStatusStep_MissingWorkflowRun(t *testing.T) {
	app, _ := newRunningArgoApp(t)

	factory := module.NewArgoStatusStepFactory()
	step, err := factory("status", map[string]any{"service": "argo-svc"}, app)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	// Empty context — no workflow_run available.
	_, err = step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err == nil {
		t.Error("expected error for missing workflow_run, got nil")
	}
}

func TestArgoLogsStep(t *testing.T) {
	app, m := newRunningArgoApp(t)

	spec := module.TranslatePipelineToArgo("ci", "argo", []map[string]any{{"name": "build"}})
	runName, err := m.SubmitWorkflow(spec)
	if err != nil {
		t.Fatalf("SubmitWorkflow: %v", err)
	}

	factory := module.NewArgoLogsStepFactory()
	step, err := factory("logs", map[string]any{
		"service":      "argo-svc",
		"workflow_run": runName,
	}, app)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}

	result, err := step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output["line_count"].(int) == 0 {
		t.Error("expected at least one log line")
	}
}

func TestArgoDeleteStep(t *testing.T) {
	app, m := newRunningArgoApp(t)

	spec := module.TranslatePipelineToArgo("ci", "argo", nil)
	runName, err := m.SubmitWorkflow(spec)
	if err != nil {
		t.Fatalf("SubmitWorkflow: %v", err)
	}

	factory := module.NewArgoDeleteStepFactory()
	step, err := factory("delete", map[string]any{
		"service":      "argo-svc",
		"workflow_run": runName,
	}, app)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}

	result, err := step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output["deleted"] != true {
		t.Errorf("expected deleted=true")
	}
}

func TestArgoListStep(t *testing.T) {
	app, m := newRunningArgoApp(t)

	spec := module.TranslatePipelineToArgo("wf", "argo", nil)
	if _, err := m.SubmitWorkflow(spec); err != nil {
		t.Fatalf("SubmitWorkflow: %v", err)
	}

	factory := module.NewArgoListStepFactory()
	step, err := factory("list", map[string]any{"service": "argo-svc"}, app)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}

	result, err := step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output["count"].(int) < 1 {
		t.Errorf("expected at least 1 workflow, got %v", result.Output["count"])
	}
}
