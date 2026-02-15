package operator

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

// validConfigYAML is a minimal but valid workflow config used across tests.
const validConfigYAML = `
modules:
  - name: httpServer
    type: http.server
    config:
      address: ":8080"
workflows:
  http:
    routes:
      - method: GET
        path: /health
        handler: httpServer
`

// invalidConfigYAML is YAML that parses but produces an invalid workflow config.
const invalidConfigYAML = `not: valid: yaml: {{`

func newTestLogger() *slog.Logger {
	return slog.Default()
}

func newTestDefinition(name, namespace string, version int, configYAML string) *WorkflowDefinition {
	return &WorkflowDefinition{
		APIVersion: "workflow.gocodalone.com/v1alpha1",
		Kind:       "WorkflowDefinition",
		Metadata: ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: WorkflowDefinitionSpec{
			Name:       name,
			Version:    version,
			ConfigYAML: configYAML,
			Replicas:   1,
		},
	}
}

// --- Reconciler Tests ---

func TestReconcileCreate(t *testing.T) {
	r := NewReconciler(newTestLogger())
	ctx := context.Background()
	def := newTestDefinition("test-workflow", "default", 1, validConfigYAML)

	result, err := r.Reconcile(ctx, def)
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}
	if result.Action != "created" {
		t.Errorf("expected action 'created', got %q", result.Action)
	}

	// Verify the definition was stored and status is Running.
	got, err := r.Get("test-workflow", "default")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got.Status.Phase != PhaseRunning {
		t.Errorf("expected phase %q, got %q", PhaseRunning, got.Status.Phase)
	}
	if got.Status.ObservedVersion != 1 {
		t.Errorf("expected observedVersion 1, got %d", got.Status.ObservedVersion)
	}
	if got.Status.ReadyReplicas != 1 {
		t.Errorf("expected readyReplicas 1, got %d", got.Status.ReadyReplicas)
	}
}

func TestReconcileUpdate(t *testing.T) {
	r := NewReconciler(newTestLogger())
	ctx := context.Background()

	// Create initial version.
	def := newTestDefinition("test-workflow", "default", 1, validConfigYAML)
	_, err := r.Reconcile(ctx, def)
	if err != nil {
		t.Fatalf("initial Reconcile failed: %v", err)
	}

	// Update to version 2.
	defV2 := newTestDefinition("test-workflow", "default", 2, validConfigYAML)
	defV2.Spec.Replicas = 3

	result, err := r.Reconcile(ctx, defV2)
	if err != nil {
		t.Fatalf("update Reconcile failed: %v", err)
	}
	if result.Action != "updated" {
		t.Errorf("expected action 'updated', got %q", result.Action)
	}

	// Verify the update was applied.
	got, err := r.Get("test-workflow", "default")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got.Status.ObservedVersion != 2 {
		t.Errorf("expected observedVersion 2, got %d", got.Status.ObservedVersion)
	}
	if got.Status.Replicas != 3 {
		t.Errorf("expected replicas 3, got %d", got.Status.Replicas)
	}
}

func TestReconcileDelete(t *testing.T) {
	r := NewReconciler(newTestLogger())
	ctx := context.Background()

	// Create a definition first.
	def := newTestDefinition("test-workflow", "default", 1, validConfigYAML)
	_, err := r.Reconcile(ctx, def)
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	// Delete it.
	err = r.Delete(ctx, "test-workflow", "default")
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Verify it is gone.
	_, err = r.Get("test-workflow", "default")
	if err == nil {
		t.Fatal("expected error after deletion, got nil")
	}
}

func TestReconcileDeleteNotFound(t *testing.T) {
	r := NewReconciler(newTestLogger())
	ctx := context.Background()

	err := r.Delete(ctx, "nonexistent", "default")
	if err == nil {
		t.Fatal("expected error deleting nonexistent definition, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error, got: %v", err)
	}
}

func TestReconcileUnchanged(t *testing.T) {
	r := NewReconciler(newTestLogger())
	ctx := context.Background()

	def := newTestDefinition("test-workflow", "default", 1, validConfigYAML)

	// First reconcile creates.
	result, err := r.Reconcile(ctx, def)
	if err != nil {
		t.Fatalf("initial Reconcile failed: %v", err)
	}
	if result.Action != "created" {
		t.Errorf("expected 'created', got %q", result.Action)
	}

	// Second reconcile with same definition should be unchanged.
	result, err = r.Reconcile(ctx, def)
	if err != nil {
		t.Fatalf("second Reconcile failed: %v", err)
	}
	if result.Action != "unchanged" {
		t.Errorf("expected action 'unchanged', got %q", result.Action)
	}
}

func TestReconcileError(t *testing.T) {
	r := NewReconciler(newTestLogger())
	ctx := context.Background()

	def := newTestDefinition("bad-workflow", "default", 1, invalidConfigYAML)

	result, err := r.Reconcile(ctx, def)
	if err == nil {
		t.Fatal("expected error for invalid config, got nil")
	}
	if result.Action != "error" {
		t.Errorf("expected action 'error', got %q", result.Action)
	}

	// Verify definition was stored with Failed phase.
	got, err := r.Get("bad-workflow", "default")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got.Status.Phase != PhaseFailed {
		t.Errorf("expected phase %q, got %q", PhaseFailed, got.Status.Phase)
	}
}

func TestReconcileEmptyConfig(t *testing.T) {
	r := NewReconciler(newTestLogger())
	ctx := context.Background()

	def := newTestDefinition("empty-config", "default", 1, "")

	result, err := r.Reconcile(ctx, def)
	if err == nil {
		t.Fatal("expected error for empty config, got nil")
	}
	if result.Action != "error" {
		t.Errorf("expected action 'error', got %q", result.Action)
	}

	got, err := r.Get("empty-config", "default")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got.Status.Phase != PhaseFailed {
		t.Errorf("expected phase %q, got %q", PhaseFailed, got.Status.Phase)
	}
}

func TestReconcileCancelledContext(t *testing.T) {
	r := NewReconciler(newTestLogger())
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	def := newTestDefinition("test-workflow", "default", 1, validConfigYAML)
	_, err := r.Reconcile(ctx, def)
	if err == nil {
		t.Fatal("expected error for cancelled context, got nil")
	}
}

func TestApplyDirect(t *testing.T) {
	r := NewReconciler(newTestLogger())
	ctx := context.Background()

	def := newTestDefinition("direct-apply", "staging", 1, validConfigYAML)

	err := r.Apply(ctx, def)
	if err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	got, err := r.Get("direct-apply", "staging")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got.Status.Phase != PhaseRunning {
		t.Errorf("expected phase %q, got %q", PhaseRunning, got.Status.Phase)
	}
}

func TestApplyDefaultsReplicas(t *testing.T) {
	r := NewReconciler(newTestLogger())
	ctx := context.Background()

	def := newTestDefinition("no-replicas", "default", 1, validConfigYAML)
	def.Spec.Replicas = 0 // should default to 1

	err := r.Apply(ctx, def)
	if err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	got, err := r.Get("no-replicas", "default")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got.Status.Replicas != 1 {
		t.Errorf("expected replicas to default to 1, got %d", got.Status.Replicas)
	}
}

func TestGetNotFound(t *testing.T) {
	r := NewReconciler(newTestLogger())

	_, err := r.Get("nonexistent", "default")
	if err == nil {
		t.Fatal("expected error for nonexistent definition, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error, got: %v", err)
	}
}

func TestListDefinitions(t *testing.T) {
	r := NewReconciler(newTestLogger())
	ctx := context.Background()

	// Deploy into two namespaces.
	defs := []*WorkflowDefinition{
		newTestDefinition("wf-a", "ns1", 1, validConfigYAML),
		newTestDefinition("wf-b", "ns1", 1, validConfigYAML),
		newTestDefinition("wf-c", "ns2", 1, validConfigYAML),
	}

	for _, def := range defs {
		if _, err := r.Reconcile(ctx, def); err != nil {
			t.Fatalf("Reconcile failed for %s: %v", def.Metadata.Name, err)
		}
	}

	// List all.
	all := r.List("")
	if len(all) != 3 {
		t.Errorf("expected 3 definitions, got %d", len(all))
	}

	// List ns1 only.
	ns1 := r.List("ns1")
	if len(ns1) != 2 {
		t.Errorf("expected 2 definitions in ns1, got %d", len(ns1))
	}

	// List ns2 only.
	ns2 := r.List("ns2")
	if len(ns2) != 1 {
		t.Errorf("expected 1 definition in ns2, got %d", len(ns2))
	}

	// List empty namespace.
	empty := r.List("nonexistent")
	if len(empty) != 0 {
		t.Errorf("expected 0 definitions in nonexistent namespace, got %d", len(empty))
	}
}

func TestListDefaultNamespace(t *testing.T) {
	r := NewReconciler(newTestLogger())
	ctx := context.Background()

	// Empty namespace should be treated as "default".
	def := newTestDefinition("wf-default", "", 1, validConfigYAML)
	_, err := r.Reconcile(ctx, def)
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	defs := r.List("default")
	if len(defs) != 1 {
		t.Errorf("expected 1 definition in default namespace, got %d", len(defs))
	}
}

// --- Controller Tests ---

func TestControllerStartStop(t *testing.T) {
	r := NewReconciler(newTestLogger())
	c := NewController(r, newTestLogger())

	ctx := context.Background()

	// Start in background.
	errCh := make(chan error, 1)
	go func() {
		errCh <- c.Start(ctx)
	}()

	// Wait for the controller to be running.
	time.Sleep(50 * time.Millisecond)
	if !c.IsRunning() {
		t.Fatal("expected controller to be running")
	}

	// Stop it.
	if err := c.Stop(); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	// Wait for Start to return.
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Start returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for controller to stop")
	}

	if c.IsRunning() {
		t.Fatal("expected controller to not be running after Stop")
	}
}

func TestControllerDoubleStart(t *testing.T) {
	r := NewReconciler(newTestLogger())
	c := NewController(r, newTestLogger())

	ctx := context.Background()
	go func() {
		_ = c.Start(ctx)
	}()
	time.Sleep(50 * time.Millisecond)
	defer func() { _ = c.Stop() }()

	// Second start should fail.
	err := c.Start(ctx)
	if err == nil {
		t.Fatal("expected error on double Start, got nil")
	}
}

func TestControllerStopNotRunning(t *testing.T) {
	r := NewReconciler(newTestLogger())
	c := NewController(r, newTestLogger())

	err := c.Stop()
	if err == nil {
		t.Fatal("expected error stopping non-running controller, got nil")
	}
}

func TestControllerProcessEvents(t *testing.T) {
	r := NewReconciler(newTestLogger())
	c := NewController(r, newTestLogger())

	ctx := context.Background()
	go func() {
		_ = c.Start(ctx)
	}()
	time.Sleep(50 * time.Millisecond)
	defer func() { _ = c.Stop() }()

	// Enqueue an ADDED event.
	def := newTestDefinition("ctrl-test", "default", 1, validConfigYAML)
	c.Enqueue(ControllerEvent{Type: EventAdded, Definition: def})

	// Wait for processing.
	time.Sleep(100 * time.Millisecond)

	got, err := r.Get("ctrl-test", "default")
	if err != nil {
		t.Fatalf("Get failed after ADDED event: %v", err)
	}
	if got.Status.Phase != PhaseRunning {
		t.Errorf("expected phase %q, got %q", PhaseRunning, got.Status.Phase)
	}

	// Enqueue a MODIFIED event with new version.
	defV2 := newTestDefinition("ctrl-test", "default", 2, validConfigYAML)
	c.Enqueue(ControllerEvent{Type: EventModified, Definition: defV2})
	time.Sleep(100 * time.Millisecond)

	got, err = r.Get("ctrl-test", "default")
	if err != nil {
		t.Fatalf("Get failed after MODIFIED event: %v", err)
	}
	if got.Status.ObservedVersion != 2 {
		t.Errorf("expected observedVersion 2, got %d", got.Status.ObservedVersion)
	}

	// Enqueue a DELETED event.
	c.Enqueue(ControllerEvent{Type: EventDeleted, Definition: def})
	time.Sleep(100 * time.Millisecond)

	_, err = r.Get("ctrl-test", "default")
	if err == nil {
		t.Fatal("expected error after DELETED event, got nil")
	}
}

func TestControllerProcessErrorEvent(t *testing.T) {
	r := NewReconciler(newTestLogger())
	c := NewController(r, newTestLogger())

	ctx := context.Background()
	go func() {
		_ = c.Start(ctx)
	}()
	time.Sleep(50 * time.Millisecond)
	defer func() { _ = c.Stop() }()

	// Enqueue a bad config.
	badDef := newTestDefinition("bad-ctrl", "default", 1, invalidConfigYAML)
	c.Enqueue(ControllerEvent{Type: EventAdded, Definition: badDef})
	time.Sleep(100 * time.Millisecond)

	got, err := r.Get("bad-ctrl", "default")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got.Status.Phase != PhaseFailed {
		t.Errorf("expected phase %q, got %q", PhaseFailed, got.Status.Phase)
	}
}

// --- Concurrency Tests ---

func TestConcurrentReconcile(t *testing.T) {
	r := NewReconciler(newTestLogger())
	ctx := context.Background()

	const numWorkers = 20
	var wg sync.WaitGroup
	errors := make(chan error, numWorkers)

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			def := newTestDefinition("concurrent-wf", "default", i+1, validConfigYAML)
			_, err := r.Reconcile(ctx, def)
			if err != nil {
				errors <- err
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Errorf("concurrent reconcile error: %v", err)
	}

	// Should have one definition stored (last write wins).
	got, err := r.Get("concurrent-wf", "default")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got.Status.Phase != PhaseRunning {
		t.Errorf("expected phase %q, got %q", PhaseRunning, got.Status.Phase)
	}
}

func TestConcurrentApplyAndDelete(t *testing.T) {
	r := NewReconciler(newTestLogger())
	ctx := context.Background()

	const numWorkflows = 10
	var wg sync.WaitGroup

	// Concurrently apply multiple different workflows.
	for i := 0; i < numWorkflows; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := "concurrent-" + string(rune('a'+i))
			def := newTestDefinition(name, "default", 1, validConfigYAML)
			_ = r.Apply(ctx, def)
		}(i)
	}
	wg.Wait()

	all := r.List("default")
	if len(all) != numWorkflows {
		t.Errorf("expected %d definitions, got %d", numWorkflows, len(all))
	}

	// Concurrently delete them all.
	for i := 0; i < numWorkflows; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := "concurrent-" + string(rune('a'+i))
			_ = r.Delete(ctx, name, "default")
		}(i)
	}
	wg.Wait()

	all = r.List("default")
	if len(all) != 0 {
		t.Errorf("expected 0 definitions after deletion, got %d", len(all))
	}
}

// --- CRD Generation Tests ---

func TestCRDGeneration(t *testing.T) {
	crd := GenerateCRD()

	if crd == "" {
		t.Fatal("GenerateCRD returned empty string")
	}

	// Verify key fields are present.
	requiredStrings := []string{
		"apiVersion: apiextensions.k8s.io/v1",
		"kind: CustomResourceDefinition",
		"workflowdefinitions.workflow.gocodalone.com",
		"workflow.gocodalone.com",
		"v1alpha1",
		"WorkflowDefinition",
		"wfdef",
		"Namespaced",
		"configYAML",
		"replicas",
		"name",
		"version",
	}

	for _, s := range requiredStrings {
		if !strings.Contains(crd, s) {
			t.Errorf("CRD YAML missing required string: %q", s)
		}
	}
}

func TestCRDContainsStatusSubresource(t *testing.T) {
	crd := GenerateCRD()
	if !strings.Contains(crd, "subresources") {
		t.Error("CRD YAML missing subresources section")
	}
	if !strings.Contains(crd, "status: {}") {
		t.Error("CRD YAML missing status subresource")
	}
}

func TestCRDContainsPrinterColumns(t *testing.T) {
	crd := GenerateCRD()
	if !strings.Contains(crd, "additionalPrinterColumns") {
		t.Error("CRD YAML missing additionalPrinterColumns")
	}
	if !strings.Contains(crd, ".status.phase") {
		t.Error("CRD YAML missing Phase printer column")
	}
}

// --- Type Tests ---

func TestDefinitionKey(t *testing.T) {
	tests := []struct {
		namespace string
		name      string
		want      string
	}{
		{"default", "my-workflow", "default/my-workflow"},
		{"", "my-workflow", "default/my-workflow"},
		{"production", "api-gw", "production/api-gw"},
	}

	for _, tt := range tests {
		got := definitionKey(tt.namespace, tt.name)
		if got != tt.want {
			t.Errorf("definitionKey(%q, %q) = %q, want %q", tt.namespace, tt.name, got, tt.want)
		}
	}
}

func TestPhaseConstants(t *testing.T) {
	if PhasePending != "Pending" {
		t.Errorf("PhasePending = %q", PhasePending)
	}
	if PhaseRunning != "Running" {
		t.Errorf("PhaseRunning = %q", PhaseRunning)
	}
	if PhaseFailed != "Failed" {
		t.Errorf("PhaseFailed = %q", PhaseFailed)
	}
	if PhaseTerminated != "Terminated" {
		t.Errorf("PhaseTerminated = %q", PhaseTerminated)
	}
}

func TestEventTypeConstants(t *testing.T) {
	if EventAdded != "ADDED" {
		t.Errorf("EventAdded = %q", EventAdded)
	}
	if EventModified != "MODIFIED" {
		t.Errorf("EventModified = %q", EventModified)
	}
	if EventDeleted != "DELETED" {
		t.Errorf("EventDeleted = %q", EventDeleted)
	}
}
