package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/api"
	"github.com/GoCodeAlone/workflow/module"
	"github.com/GoCodeAlone/workflow/observability"
	"github.com/GoCodeAlone/workflow/store"
	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// Mock trigger infrastructure for CrossWorkflowRouter tests
// ---------------------------------------------------------------------------

type e2eMockTriggerWorkflower struct {
	mu    sync.Mutex
	calls []e2eTriggerCall
}

type e2eTriggerCall struct {
	workflowType string
	action       string
	data         map[string]interface{}
}

func (m *e2eMockTriggerWorkflower) TriggerWorkflow(_ context.Context, workflowType string, action string, data map[string]interface{}) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, e2eTriggerCall{workflowType, action, data})
	return nil
}

func (m *e2eMockTriggerWorkflower) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

// e2eMockManagedEngine satisfies module.triggerableEngine (duck-typed).
type e2eMockManagedEngine struct {
	tw *e2eMockTriggerWorkflower
}

func (m *e2eMockManagedEngine) GetEngine() module.TriggerWorkflower {
	return m.tw
}

// ---------------------------------------------------------------------------
// e2eFixture holds all stores and pre-created entities for tests
// ---------------------------------------------------------------------------

type e2eFixture struct {
	userStore       *store.MockUserStore
	companyStore    *store.MockCompanyStore
	projectStore    *store.MockProjectStore
	workflowStore   *store.MockWorkflowStore
	membershipStore *store.MockMembershipStore
	linkStore       *store.MockCrossWorkflowLinkStore
	sessionStore    *store.MockSessionStore
	executionStore  *store.MockExecutionStore
	logStore        *store.MockLogStore
	auditStore      *store.MockAuditStore

	// Pre-created entities
	alice, bob, carol                     *store.User
	acmeCorp                              *store.Company
	ecommerceProject                      *store.Project
	workflowA, workflowB, workflowC       *store.WorkflowRecord
	workflowAID, workflowBID, workflowCID uuid.UUID

	// Services
	permissionSvc *api.PermissionService
	tracker       *observability.ExecutionTracker
}

func setupE2EFixture(t *testing.T) *e2eFixture {
	t.Helper()
	ctx := context.Background()

	f := &e2eFixture{}

	// Create all mock stores
	f.userStore = store.NewMockUserStore()
	f.companyStore = store.NewMockCompanyStore()
	f.projectStore = store.NewMockProjectStore()
	f.workflowStore = store.NewMockWorkflowStore()
	f.membershipStore = store.NewMockMembershipStore()
	f.linkStore = store.NewMockCrossWorkflowLinkStore()
	f.sessionStore = store.NewMockSessionStore()
	f.executionStore = store.NewMockExecutionStore()
	f.logStore = store.NewMockLogStore()
	f.auditStore = store.NewMockAuditStore()

	// Wire up cross-references for ListForUser
	f.companyStore.SetMembershipStore(f.membershipStore)
	f.projectStore.SetMembershipStore(f.membershipStore)

	// --- Create users ---
	f.alice = &store.User{Email: "alice@acme.com", DisplayName: "Alice Owner", Active: true}
	f.bob = &store.User{Email: "bob@acme.com", DisplayName: "Bob Editor", Active: true}
	f.carol = &store.User{Email: "carol@acme.com", DisplayName: "Carol Viewer", Active: true}
	mustNoErr(t, f.userStore.Create(ctx, f.alice))
	mustNoErr(t, f.userStore.Create(ctx, f.bob))
	mustNoErr(t, f.userStore.Create(ctx, f.carol))

	// --- Create company ---
	f.acmeCorp = &store.Company{Name: "Acme Corp", Slug: "acme-corp", OwnerID: f.alice.ID}
	mustNoErr(t, f.companyStore.Create(ctx, f.acmeCorp))

	// --- Create project ---
	f.ecommerceProject = &store.Project{
		CompanyID:   f.acmeCorp.ID,
		Name:        "E-Commerce",
		Slug:        "e-commerce",
		Description: "Online store workflows",
	}
	mustNoErr(t, f.projectStore.Create(ctx, f.ecommerceProject))

	// --- Create workflows ---
	f.workflowA = &store.WorkflowRecord{
		ProjectID:  f.ecommerceProject.ID,
		Name:       "Order Validation",
		Slug:       "order-validation",
		ConfigYAML: "name: order-validation\nversion: 1\nmodules: []\ntriggers: []\nworkflows: []",
		Status:     store.WorkflowStatusDraft,
		CreatedBy:  f.alice.ID,
		UpdatedBy:  f.alice.ID,
	}
	f.workflowB = &store.WorkflowRecord{
		ProjectID:  f.ecommerceProject.ID,
		Name:       "Order Fulfillment",
		Slug:       "order-fulfillment",
		ConfigYAML: "name: order-fulfillment\nversion: 1\nmodules: []\ntriggers: []\nworkflows: []",
		Status:     store.WorkflowStatusDraft,
		CreatedBy:  f.alice.ID,
		UpdatedBy:  f.alice.ID,
	}
	f.workflowC = &store.WorkflowRecord{
		ProjectID:  f.ecommerceProject.ID,
		Name:       "Shipping Notification",
		Slug:       "shipping-notification",
		ConfigYAML: "name: shipping-notification\nversion: 1\nmodules: []\ntriggers: []\nworkflows: []",
		Status:     store.WorkflowStatusDraft,
		CreatedBy:  f.carol.ID, // Carol created this one
		UpdatedBy:  f.carol.ID,
	}
	mustNoErr(t, f.workflowStore.Create(ctx, f.workflowA))
	mustNoErr(t, f.workflowStore.Create(ctx, f.workflowB))
	mustNoErr(t, f.workflowStore.Create(ctx, f.workflowC))
	f.workflowAID = f.workflowA.ID
	f.workflowBID = f.workflowB.ID
	f.workflowCID = f.workflowC.ID

	// --- Set up memberships ---
	// Alice: company-level owner
	mustNoErr(t, f.membershipStore.Create(ctx, &store.Membership{
		UserID:    f.alice.ID,
		CompanyID: f.acmeCorp.ID,
		Role:      store.RoleOwner,
	}))
	// Bob: project-level editor
	projID := f.ecommerceProject.ID
	mustNoErr(t, f.membershipStore.Create(ctx, &store.Membership{
		UserID:    f.bob.ID,
		CompanyID: f.acmeCorp.ID,
		ProjectID: &projID,
		Role:      store.RoleEditor,
	}))
	// Carol: NO membership (she only has access to workflowC via CreatedBy)

	// --- Create services ---
	f.permissionSvc = api.NewPermissionService(f.membershipStore, f.workflowStore, f.projectStore)
	f.tracker = observability.NewExecutionTracker(f.executionStore, f.logStore)

	return f
}

func mustNoErr(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// ===========================================================================
// TestMultiWorkflowE2E_Setup
// ===========================================================================

func TestMultiWorkflowE2E_Setup(t *testing.T) {
	ctx := context.Background()
	f := setupE2EFixture(t)

	// Verify users
	users, err := f.userStore.List(ctx, store.UserFilter{})
	mustNoErr(t, err)
	if len(users) != 3 {
		t.Fatalf("expected 3 users, got %d", len(users))
	}

	// Verify company
	co, err := f.companyStore.GetBySlug(ctx, "acme-corp")
	mustNoErr(t, err)
	if co.OwnerID != f.alice.ID {
		t.Fatal("acme-corp should be owned by alice")
	}

	// Verify project
	proj, err := f.projectStore.GetBySlug(ctx, f.acmeCorp.ID, "e-commerce")
	mustNoErr(t, err)
	if proj.CompanyID != f.acmeCorp.ID {
		t.Fatal("project should belong to acme-corp")
	}

	// Verify 3 workflows
	wfs, err := f.workflowStore.List(ctx, store.WorkflowFilter{ProjectID: &proj.ID})
	mustNoErr(t, err)
	if len(wfs) != 3 {
		t.Fatalf("expected 3 workflows, got %d", len(wfs))
	}

	// Verify workflow versions start at 1
	for _, wf := range wfs {
		if wf.Version != 1 {
			t.Fatalf("expected version 1, got %d for %s", wf.Version, wf.Slug)
		}
	}

	// Verify memberships
	memberships, err := f.membershipStore.List(ctx, store.MembershipFilter{})
	mustNoErr(t, err)
	if len(memberships) != 2 {
		t.Fatalf("expected 2 memberships (alice + bob), got %d", len(memberships))
	}

	// Verify tracker and permission service are not nil
	if f.tracker == nil {
		t.Fatal("tracker should be initialized")
	}
	if f.permissionSvc == nil {
		t.Fatal("permissionSvc should be initialized")
	}
}

// ===========================================================================
// TestMultiWorkflowE2E_PermissionIsolation
// ===========================================================================

func TestMultiWorkflowE2E_PermissionIsolation(t *testing.T) {
	ctx := context.Background()
	f := setupE2EFixture(t)

	t.Run("Alice_OwnerAccessAll", func(t *testing.T) {
		// Alice is company-level owner, so she can access all workflows.
		// Also, she created workflows A and B, giving her owner role via CreatedBy.
		for _, wfID := range []uuid.UUID{f.workflowAID, f.workflowBID, f.workflowCID} {
			if !f.permissionSvc.CanAccess(ctx, f.alice.ID, "workflow", wfID, store.RoleViewer) {
				t.Fatalf("Alice should have viewer access to workflow %s", wfID)
			}
		}
		// Alice should have owner-level on A and B (CreatedBy) and owner on C (company cascade)
		roleA, _ := f.permissionSvc.GetEffectiveRole(ctx, f.alice.ID, "workflow", f.workflowAID)
		if roleA != store.RoleOwner {
			t.Fatalf("expected owner on wfA (CreatedBy), got %s", roleA)
		}
		roleC, _ := f.permissionSvc.GetEffectiveRole(ctx, f.alice.ID, "workflow", f.workflowCID)
		if roleC != store.RoleOwner {
			t.Fatalf("expected owner on wfC (company cascade), got %s", roleC)
		}
	})

	t.Run("Bob_EditorAccessAll", func(t *testing.T) {
		// Bob has project-level editor membership; all 3 workflows are in that project.
		for _, wfID := range []uuid.UUID{f.workflowAID, f.workflowBID, f.workflowCID} {
			if !f.permissionSvc.CanAccess(ctx, f.bob.ID, "workflow", wfID, store.RoleViewer) {
				t.Fatalf("Bob should have viewer access to workflow %s", wfID)
			}
			if !f.permissionSvc.CanAccess(ctx, f.bob.ID, "workflow", wfID, store.RoleEditor) {
				t.Fatalf("Bob should have editor access to workflow %s", wfID)
			}
		}
		// Bob should NOT have owner-level access (he didn't create any of these)
		if f.permissionSvc.CanAccess(ctx, f.bob.ID, "workflow", f.workflowAID, store.RoleOwner) {
			t.Fatal("Bob should NOT have owner access to wfA")
		}
	})

	t.Run("Carol_OnlyWorkflowC", func(t *testing.T) {
		// Carol has NO membership but she created workflowC, giving her owner role on C.
		if !f.permissionSvc.CanAccess(ctx, f.carol.ID, "workflow", f.workflowCID, store.RoleOwner) {
			t.Fatal("Carol should have owner access to wfC (she created it)")
		}
		// Carol should NOT have access to workflow A or B
		if f.permissionSvc.CanAccess(ctx, f.carol.ID, "workflow", f.workflowAID, store.RoleViewer) {
			t.Fatal("Carol should NOT have access to wfA")
		}
		if f.permissionSvc.CanAccess(ctx, f.carol.ID, "workflow", f.workflowBID, store.RoleViewer) {
			t.Fatal("Carol should NOT have access to wfB")
		}
	})

	t.Run("UnknownUser_NoAccess", func(t *testing.T) {
		stranger := uuid.New()
		for _, wfID := range []uuid.UUID{f.workflowAID, f.workflowBID, f.workflowCID} {
			if f.permissionSvc.CanAccess(ctx, stranger, "workflow", wfID, store.RoleViewer) {
				t.Fatalf("stranger should NOT have access to workflow %s", wfID)
			}
		}
	})

	t.Run("RoleHierarchy", func(t *testing.T) {
		// Verify role hierarchy: owner > admin > editor > viewer
		if !api.RoleAtLeast(store.RoleOwner, store.RoleViewer) {
			t.Fatal("owner should be at least viewer")
		}
		if !api.RoleAtLeast(store.RoleOwner, store.RoleAdmin) {
			t.Fatal("owner should be at least admin")
		}
		if api.RoleAtLeast(store.RoleViewer, store.RoleEditor) {
			t.Fatal("viewer should NOT be at least editor")
		}
		if api.RoleAtLeast(store.RoleEditor, store.RoleAdmin) {
			t.Fatal("editor should NOT be at least admin")
		}
	})
}

// ===========================================================================
// TestMultiWorkflowE2E_CrossWorkflowRouting
// ===========================================================================

func TestMultiWorkflowE2E_CrossWorkflowRouting(t *testing.T) {
	ctx := context.Background()
	f := setupE2EFixture(t)

	// Create cross-workflow links: A->B (order.validated), B->C (fulfillment.*)
	mustNoErr(t, f.linkStore.Create(ctx, &store.CrossWorkflowLink{
		SourceWorkflowID: f.workflowAID,
		TargetWorkflowID: f.workflowBID,
		LinkType:         "order.validated",
		CreatedBy:        f.alice.ID,
	}))
	mustNoErr(t, f.linkStore.Create(ctx, &store.CrossWorkflowLink{
		SourceWorkflowID: f.workflowBID,
		TargetWorkflowID: f.workflowCID,
		LinkType:         "fulfillment.*",
		CreatedBy:        f.alice.ID,
	}))

	// Create mock engines for each workflow
	twA := &e2eMockTriggerWorkflower{}
	twB := &e2eMockTriggerWorkflower{}
	twC := &e2eMockTriggerWorkflower{}
	meA := &e2eMockManagedEngine{tw: twA}
	meB := &e2eMockManagedEngine{tw: twB}
	meC := &e2eMockManagedEngine{tw: twC}

	engines := map[uuid.UUID]interface{}{
		f.workflowAID: meA,
		f.workflowBID: meB,
		f.workflowCID: meC,
	}

	router := module.NewCrossWorkflowRouter(f.linkStore, func(id uuid.UUID) (interface{}, bool) {
		e, ok := engines[id]
		return e, ok
	}, discardLogger())

	mustNoErr(t, router.RefreshLinks(ctx))

	t.Run("OrderValidated_RoutesToB", func(t *testing.T) {
		mustNoErr(t, router.RouteEvent(ctx, f.workflowAID, "order.validated", map[string]interface{}{"order_id": "ORD-001"}))
		if twB.callCount() != 1 {
			t.Fatalf("expected 1 call to workflow B, got %d", twB.callCount())
		}
		if twA.callCount() != 0 {
			t.Fatal("workflow A engine should not be triggered")
		}
		if twC.callCount() != 0 {
			t.Fatal("workflow C engine should not be triggered")
		}
	})

	t.Run("FulfillmentCompleted_RoutesToC", func(t *testing.T) {
		mustNoErr(t, router.RouteEvent(ctx, f.workflowBID, "fulfillment.completed", map[string]interface{}{"order_id": "ORD-001"}))
		if twC.callCount() != 1 {
			t.Fatalf("expected 1 call to workflow C, got %d", twC.callCount())
		}
	})

	t.Run("FulfillmentShipped_RoutesToC", func(t *testing.T) {
		mustNoErr(t, router.RouteEvent(ctx, f.workflowBID, "fulfillment.shipped", map[string]interface{}{"tracking": "TRK-123"}))
		if twC.callCount() != 2 {
			t.Fatalf("expected 2 total calls to workflow C, got %d", twC.callCount())
		}
	})

	t.Run("OrderCancelled_NoRouting", func(t *testing.T) {
		beforeB := twB.callCount()
		beforeC := twC.callCount()
		mustNoErr(t, router.RouteEvent(ctx, f.workflowAID, "order.cancelled", nil))
		if twB.callCount() != beforeB {
			t.Fatal("order.cancelled should NOT route to B (pattern is order.validated)")
		}
		if twC.callCount() != beforeC {
			t.Fatal("order.cancelled should NOT route to C")
		}
	})

	t.Run("RandomEvent_NoRouting", func(t *testing.T) {
		beforeB := twB.callCount()
		beforeC := twC.callCount()
		mustNoErr(t, router.RouteEvent(ctx, f.workflowAID, "random.event", nil))
		if twB.callCount() != beforeB {
			t.Fatal("random.event should NOT route anywhere")
		}
		if twC.callCount() != beforeC {
			t.Fatal("random.event should NOT route anywhere")
		}
	})
}

// ===========================================================================
// TestMultiWorkflowE2E_DataFlowAndPersistence
// ===========================================================================

func TestMultiWorkflowE2E_DataFlowAndPersistence(t *testing.T) {
	ctx := context.Background()
	f := setupE2EFixture(t)

	triggerData, _ := json.Marshal(map[string]interface{}{
		"order_id": "ORD-001",
		"customer": "john@example.com",
		"total":    99.99,
	})

	// Start execution
	execID, err := f.tracker.StartExecution(ctx, f.workflowAID, "http", triggerData)
	mustNoErr(t, err)
	if execID == uuid.Nil {
		t.Fatal("expected valid execution ID")
	}

	// Small delay to ensure duration > 0
	time.Sleep(5 * time.Millisecond)

	// Record steps
	now := time.Now()
	mustNoErr(t, f.tracker.RecordStep(ctx, execID, &store.ExecutionStep{
		StepName:    "validate",
		StepType:    "action",
		Status:      store.StepStatusCompleted,
		SequenceNum: 0,
		StartedAt:   &now,
		CompletedAt: &now,
	}))
	mustNoErr(t, f.tracker.RecordStep(ctx, execID, &store.ExecutionStep{
		StepName:    "transform",
		StepType:    "action",
		Status:      store.StepStatusCompleted,
		SequenceNum: 1,
		StartedAt:   &now,
		CompletedAt: &now,
	}))
	mustNoErr(t, f.tracker.RecordStep(ctx, execID, &store.ExecutionStep{
		StepName:    "persist",
		StepType:    "action",
		Status:      store.StepStatusCompleted,
		SequenceNum: 2,
		StartedAt:   &now,
		CompletedAt: &now,
	}))

	// Complete execution
	outputData, _ := json.Marshal(map[string]interface{}{
		"order_id": "ORD-001",
		"status":   "validated",
	})
	mustNoErr(t, f.tracker.CompleteExecution(ctx, execID, outputData))

	// Verify execution record
	exec, err := f.executionStore.GetExecution(ctx, execID)
	mustNoErr(t, err)
	if exec.Status != store.ExecutionStatusCompleted {
		t.Fatalf("expected completed status, got %s", exec.Status)
	}
	if exec.CompletedAt == nil {
		t.Fatal("expected CompletedAt to be set")
	}
	if exec.DurationMs == nil || *exec.DurationMs <= 0 {
		t.Fatalf("expected duration_ms > 0, got %v", exec.DurationMs)
	}

	// Verify trigger data persisted
	var triggerMap map[string]interface{}
	mustNoErr(t, json.Unmarshal(exec.TriggerData, &triggerMap))
	if triggerMap["order_id"] != "ORD-001" {
		t.Fatal("trigger data order_id mismatch")
	}

	// Verify output data persisted
	var outputMap map[string]interface{}
	mustNoErr(t, json.Unmarshal(exec.OutputData, &outputMap))
	if outputMap["status"] != "validated" {
		t.Fatal("output data status mismatch")
	}

	// Verify 3 steps in sequence
	steps, err := f.executionStore.ListSteps(ctx, execID)
	mustNoErr(t, err)
	if len(steps) != 3 {
		t.Fatalf("expected 3 steps, got %d", len(steps))
	}
	expectedSteps := []string{"validate", "transform", "persist"}
	for i, step := range steps {
		if step.StepName != expectedSteps[i] {
			t.Fatalf("step %d: expected %s, got %s", i, expectedSteps[i], step.StepName)
		}
		if step.SequenceNum != i {
			t.Fatalf("step %d: expected sequence %d, got %d", i, i, step.SequenceNum)
		}
		if step.Status != store.StepStatusCompleted {
			t.Fatalf("step %d: expected completed, got %s", i, step.Status)
		}
	}
}

// ===========================================================================
// TestMultiWorkflowE2E_ExecutionTracking
// ===========================================================================

func TestMultiWorkflowE2E_ExecutionTracking(t *testing.T) {
	ctx := context.Background()
	f := setupE2EFixture(t)

	triggerData, _ := json.Marshal(map[string]interface{}{"order_id": "ORD-TRACK"})

	// Execution 1: completed successfully
	exec1ID, err := f.tracker.StartExecution(ctx, f.workflowAID, "http", triggerData)
	mustNoErr(t, err)
	time.Sleep(2 * time.Millisecond)
	mustNoErr(t, f.tracker.CompleteExecution(ctx, exec1ID, json.RawMessage(`{"result":"ok"}`)))

	// Write some info logs for execution 1
	infoWriter := f.tracker.LogWriter(f.workflowAID, exec1ID, store.LogLevelInfo)
	_, _ = fmt.Fprint(infoWriter, "Processing order ORD-TRACK")
	_, _ = fmt.Fprint(infoWriter, "Validation passed")

	// Execution 2: failed with error
	exec2ID, err := f.tracker.StartExecution(ctx, f.workflowAID, "http", triggerData)
	mustNoErr(t, err)
	time.Sleep(2 * time.Millisecond)
	mustNoErr(t, f.tracker.FailExecution(ctx, exec2ID, errors.New("payment gateway timeout")))

	// Write error log for execution 2
	errorWriter := f.tracker.LogWriter(f.workflowAID, exec2ID, store.LogLevelError)
	_, _ = fmt.Fprint(errorWriter, "Payment processing failed: gateway timeout")

	// Execution 3: cancelled
	exec3ID, err := f.tracker.StartExecution(ctx, f.workflowAID, "cron", triggerData)
	mustNoErr(t, err)
	time.Sleep(2 * time.Millisecond)
	mustNoErr(t, f.tracker.CancelExecution(ctx, exec3ID))

	// Write warn log for execution 3
	warnWriter := f.tracker.LogWriter(f.workflowAID, exec3ID, store.LogLevelWarn)
	_, _ = fmt.Fprint(warnWriter, "Execution was cancelled by user")

	t.Run("ListAllExecutions", func(t *testing.T) {
		wfID := f.workflowAID
		execs, err := f.executionStore.ListExecutions(ctx, store.ExecutionFilter{WorkflowID: &wfID})
		mustNoErr(t, err)
		if len(execs) != 3 {
			t.Fatalf("expected 3 executions, got %d", len(execs))
		}
	})

	t.Run("FilterByStatusCompleted", func(t *testing.T) {
		wfID := f.workflowAID
		execs, err := f.executionStore.ListExecutions(ctx, store.ExecutionFilter{
			WorkflowID: &wfID,
			Status:     store.ExecutionStatusCompleted,
		})
		mustNoErr(t, err)
		if len(execs) != 1 {
			t.Fatalf("expected 1 completed, got %d", len(execs))
		}
		if execs[0].ID != exec1ID {
			t.Fatal("expected execution 1")
		}
	})

	t.Run("FilterByStatusFailed", func(t *testing.T) {
		wfID := f.workflowAID
		execs, err := f.executionStore.ListExecutions(ctx, store.ExecutionFilter{
			WorkflowID: &wfID,
			Status:     store.ExecutionStatusFailed,
		})
		mustNoErr(t, err)
		if len(execs) != 1 {
			t.Fatalf("expected 1 failed, got %d", len(execs))
		}
		failedExec := execs[0]
		if failedExec.ErrorMessage != "payment gateway timeout" {
			t.Fatalf("expected error message, got %q", failedExec.ErrorMessage)
		}
	})

	t.Run("CountByStatus", func(t *testing.T) {
		counts, err := f.executionStore.CountByStatus(ctx, f.workflowAID)
		mustNoErr(t, err)
		if counts[store.ExecutionStatusCompleted] != 1 {
			t.Fatalf("expected 1 completed, got %d", counts[store.ExecutionStatusCompleted])
		}
		if counts[store.ExecutionStatusFailed] != 1 {
			t.Fatalf("expected 1 failed, got %d", counts[store.ExecutionStatusFailed])
		}
		if counts[store.ExecutionStatusCancelled] != 1 {
			t.Fatalf("expected 1 cancelled, got %d", counts[store.ExecutionStatusCancelled])
		}
	})

	t.Run("QueryLogsByLevelError", func(t *testing.T) {
		errorLogs, err := f.logStore.Query(ctx, store.LogFilter{Level: store.LogLevelError})
		mustNoErr(t, err)
		if len(errorLogs) != 1 {
			t.Fatalf("expected 1 error log, got %d", len(errorLogs))
		}
		if errorLogs[0].Message != "Payment processing failed: gateway timeout" {
			t.Fatalf("unexpected error log message: %q", errorLogs[0].Message)
		}
	})

	t.Run("CountByLevel", func(t *testing.T) {
		counts, err := f.logStore.CountByLevel(ctx, f.workflowAID)
		mustNoErr(t, err)
		if counts[store.LogLevelInfo] != 2 {
			t.Fatalf("expected 2 info logs, got %d", counts[store.LogLevelInfo])
		}
		if counts[store.LogLevelError] != 1 {
			t.Fatalf("expected 1 error log, got %d", counts[store.LogLevelError])
		}
		if counts[store.LogLevelWarn] != 1 {
			t.Fatalf("expected 1 warn log, got %d", counts[store.LogLevelWarn])
		}
	})

	t.Run("ExecutionDurationSet", func(t *testing.T) {
		exec, err := f.executionStore.GetExecution(ctx, exec1ID)
		mustNoErr(t, err)
		if exec.DurationMs == nil {
			t.Fatal("expected duration_ms to be set")
		}
		if *exec.DurationMs < 0 {
			t.Fatalf("expected non-negative duration_ms, got %d", *exec.DurationMs)
		}
	})
}

// ===========================================================================
// TestMultiWorkflowE2E_AuditTrail
// ===========================================================================

func TestMultiWorkflowE2E_AuditTrail(t *testing.T) {
	ctx := context.Background()
	f := setupE2EFixture(t)

	aliceID := f.alice.ID
	bobID := f.bob.ID
	wfAID := f.workflowAID
	wfBID := f.workflowBID

	// Record audit entries
	mustNoErr(t, f.auditStore.Record(ctx, &store.AuditEntry{
		UserID:       &aliceID,
		Action:       "deploy",
		ResourceType: "workflow",
		ResourceID:   &wfAID,
		IPAddress:    "10.0.0.1",
	}))
	mustNoErr(t, f.auditStore.Record(ctx, &store.AuditEntry{
		UserID:       &aliceID,
		Action:       "stop",
		ResourceType: "workflow",
		ResourceID:   &wfBID,
		IPAddress:    "10.0.0.1",
	}))
	mustNoErr(t, f.auditStore.Record(ctx, &store.AuditEntry{
		UserID:       &bobID,
		Action:       "deploy",
		ResourceType: "workflow",
		ResourceID:   &wfBID,
		IPAddress:    "10.0.0.2",
	}))
	mustNoErr(t, f.auditStore.Record(ctx, &store.AuditEntry{
		UserID:       &aliceID,
		Action:       "share",
		ResourceType: "workflow",
		ResourceID:   &wfAID,
		IPAddress:    "10.0.0.1",
	}))

	t.Run("QueryByUser_Alice", func(t *testing.T) {
		results, err := f.auditStore.Query(ctx, store.AuditFilter{UserID: &aliceID})
		mustNoErr(t, err)
		if len(results) != 3 {
			t.Fatalf("expected 3 entries for alice, got %d", len(results))
		}
	})

	t.Run("QueryByUser_Bob", func(t *testing.T) {
		results, err := f.auditStore.Query(ctx, store.AuditFilter{UserID: &bobID})
		mustNoErr(t, err)
		if len(results) != 1 {
			t.Fatalf("expected 1 entry for bob, got %d", len(results))
		}
	})

	t.Run("QueryByResource_WorkflowA", func(t *testing.T) {
		results, err := f.auditStore.Query(ctx, store.AuditFilter{ResourceID: &wfAID})
		mustNoErr(t, err)
		if len(results) != 2 {
			t.Fatalf("expected 2 entries for workflowA, got %d", len(results))
		}
	})

	t.Run("QueryByAction_Deploy", func(t *testing.T) {
		results, err := f.auditStore.Query(ctx, store.AuditFilter{Action: "deploy"})
		mustNoErr(t, err)
		if len(results) != 2 {
			t.Fatalf("expected 2 deploy entries, got %d", len(results))
		}
	})

	t.Run("QueryByAction_Share", func(t *testing.T) {
		results, err := f.auditStore.Query(ctx, store.AuditFilter{Action: "share"})
		mustNoErr(t, err)
		if len(results) != 1 {
			t.Fatalf("expected 1 share entry, got %d", len(results))
		}
	})

	t.Run("QueryCombined_AliceDeploy", func(t *testing.T) {
		results, err := f.auditStore.Query(ctx, store.AuditFilter{UserID: &aliceID, Action: "deploy"})
		mustNoErr(t, err)
		if len(results) != 1 {
			t.Fatalf("expected 1 alice+deploy entry, got %d", len(results))
		}
	})

	t.Run("AutoIncrementIDs", func(t *testing.T) {
		results, err := f.auditStore.Query(ctx, store.AuditFilter{})
		mustNoErr(t, err)
		for i := 1; i < len(results); i++ {
			if results[i].ID <= results[i-1].ID {
				t.Fatal("expected strictly increasing audit IDs")
			}
		}
	})
}

// ===========================================================================
// TestMultiWorkflowE2E_StateQueryable
// ===========================================================================

func TestMultiWorkflowE2E_StateQueryable(t *testing.T) {
	ctx := context.Background()
	f := setupE2EFixture(t)

	// 1. Create workflow and "deploy" it (update status to active)
	wf := f.workflowA
	wfCopy := *wf
	wfCopy.Status = store.WorkflowStatusActive
	mustNoErr(t, f.workflowStore.Update(ctx, &wfCopy))

	// Verify queryable by status
	activeWFs, err := f.workflowStore.List(ctx, store.WorkflowFilter{Status: store.WorkflowStatusActive})
	mustNoErr(t, err)
	if len(activeWFs) != 1 {
		t.Fatalf("expected 1 active workflow, got %d", len(activeWFs))
	}
	if activeWFs[0].Slug != "order-validation" {
		t.Fatalf("expected order-validation, got %s", activeWFs[0].Slug)
	}

	// 2. Run an execution with steps and logs
	triggerData, _ := json.Marshal(map[string]interface{}{"order_id": "ORD-QUERY"})
	execID, err := f.tracker.StartExecution(ctx, f.workflowAID, "http", triggerData)
	mustNoErr(t, err)

	// Log some messages
	infoWriter := f.tracker.LogWriter(f.workflowAID, execID, store.LogLevelInfo)
	_, _ = fmt.Fprint(infoWriter, "Starting order processing")
	debugWriter := f.tracker.LogWriter(f.workflowAID, execID, store.LogLevelDebug)
	_, _ = fmt.Fprint(debugWriter, "Input data parsed")

	// Record steps
	now := time.Now()
	mustNoErr(t, f.tracker.RecordStep(ctx, execID, &store.ExecutionStep{
		StepName:    "parse-input",
		StepType:    "transform",
		Status:      store.StepStatusCompleted,
		SequenceNum: 0,
		StartedAt:   &now,
		CompletedAt: &now,
	}))
	mustNoErr(t, f.tracker.RecordStep(ctx, execID, &store.ExecutionStep{
		StepName:    "validate-order",
		StepType:    "action",
		Status:      store.StepStatusCompleted,
		SequenceNum: 1,
		StartedAt:   &now,
		CompletedAt: &now,
	}))
	mustNoErr(t, f.tracker.RecordStep(ctx, execID, &store.ExecutionStep{
		StepName:    "persist-result",
		StepType:    "action",
		Status:      store.StepStatusCompleted,
		SequenceNum: 2,
		StartedAt:   &now,
		CompletedAt: &now,
	}))

	time.Sleep(2 * time.Millisecond)
	mustNoErr(t, f.tracker.CompleteExecution(ctx, execID, json.RawMessage(`{"status":"processed"}`)))

	// 3. Prove everything is queryable

	t.Run("QueryWorkflowByStatus", func(t *testing.T) {
		results, err := f.workflowStore.List(ctx, store.WorkflowFilter{Status: store.WorkflowStatusActive})
		mustNoErr(t, err)
		if len(results) != 1 {
			t.Fatalf("expected 1 active workflow, got %d", len(results))
		}
	})

	t.Run("QueryVersionHistory", func(t *testing.T) {
		versions, err := f.workflowStore.ListVersions(ctx, f.workflowAID)
		mustNoErr(t, err)
		if len(versions) < 2 {
			t.Fatalf("expected at least 2 versions (draft + active), got %d", len(versions))
		}
		// First version should be draft, last should be active
		if versions[0].Status != store.WorkflowStatusDraft {
			t.Fatalf("expected first version to be draft, got %s", versions[0].Status)
		}
		if versions[len(versions)-1].Status != store.WorkflowStatusActive {
			t.Fatalf("expected last version to be active, got %s", versions[len(versions)-1].Status)
		}
	})

	t.Run("QueryExecutionsByWorkflow", func(t *testing.T) {
		wfID := f.workflowAID
		execs, err := f.executionStore.ListExecutions(ctx, store.ExecutionFilter{WorkflowID: &wfID})
		mustNoErr(t, err)
		if len(execs) != 1 {
			t.Fatalf("expected 1 execution, got %d", len(execs))
		}
		if execs[0].Status != store.ExecutionStatusCompleted {
			t.Fatalf("expected completed, got %s", execs[0].Status)
		}
	})

	t.Run("QueryStepsByExecution", func(t *testing.T) {
		steps, err := f.executionStore.ListSteps(ctx, execID)
		mustNoErr(t, err)
		if len(steps) != 3 {
			t.Fatalf("expected 3 steps, got %d", len(steps))
		}
		// Verify ordering
		if steps[0].StepName != "parse-input" {
			t.Fatalf("expected first step parse-input, got %s", steps[0].StepName)
		}
		if steps[1].StepName != "validate-order" {
			t.Fatalf("expected second step validate-order, got %s", steps[1].StepName)
		}
		if steps[2].StepName != "persist-result" {
			t.Fatalf("expected third step persist-result, got %s", steps[2].StepName)
		}
	})

	t.Run("QueryLogsByExecution", func(t *testing.T) {
		logs, err := f.logStore.Query(ctx, store.LogFilter{ExecutionID: &execID})
		mustNoErr(t, err)
		if len(logs) != 2 {
			t.Fatalf("expected 2 execution-scoped logs, got %d", len(logs))
		}
	})

	t.Run("QueryLogsByWorkflow", func(t *testing.T) {
		wfID := f.workflowAID
		logs, err := f.logStore.Query(ctx, store.LogFilter{WorkflowID: &wfID})
		mustNoErr(t, err)
		if len(logs) != 2 {
			t.Fatalf("expected 2 logs for workflow, got %d", len(logs))
		}
	})

	t.Run("ExecutionOutput", func(t *testing.T) {
		exec, err := f.executionStore.GetExecution(ctx, execID)
		mustNoErr(t, err)
		var out map[string]interface{}
		mustNoErr(t, json.Unmarshal(exec.OutputData, &out))
		if out["status"] != "processed" {
			t.Fatalf("expected status=processed, got %v", out["status"])
		}
	})

	t.Run("DataEntered_Transformed_Persisted_Queryable", func(t *testing.T) {
		// This is the crown jewel assertion: prove the full data flow.
		// Data entered via trigger -> transformed through steps -> persisted as output -> queryable.

		// 1. Original trigger data is queryable
		exec, err := f.executionStore.GetExecution(ctx, execID)
		mustNoErr(t, err)
		var trigData map[string]interface{}
		mustNoErr(t, json.Unmarshal(exec.TriggerData, &trigData))
		if trigData["order_id"] != "ORD-QUERY" {
			t.Fatal("trigger data not persisted correctly")
		}

		// 2. Steps recorded the transformation chain
		steps, err := f.executionStore.ListSteps(ctx, execID)
		mustNoErr(t, err)
		if len(steps) != 3 {
			t.Fatal("transformation steps not recorded")
		}

		// 3. Output reflects transformed state
		var outData map[string]interface{}
		mustNoErr(t, json.Unmarshal(exec.OutputData, &outData))
		if outData["status"] != "processed" {
			t.Fatal("output state not persisted correctly")
		}

		// 4. Execution metadata is complete
		if exec.Status != store.ExecutionStatusCompleted {
			t.Fatal("execution status incorrect")
		}
		if exec.DurationMs == nil || *exec.DurationMs < 0 {
			t.Fatal("execution duration not tracked")
		}

		// 5. Logs captured during execution are queryable
		logs, err := f.logStore.Query(ctx, store.LogFilter{ExecutionID: &execID})
		mustNoErr(t, err)
		if len(logs) == 0 {
			t.Fatal("execution logs not captured")
		}
	})
}
