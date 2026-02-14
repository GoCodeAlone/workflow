package module

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"

	"github.com/GoCodeAlone/workflow/store"
	"github.com/google/uuid"
)

// --- matchPattern tests ---

func TestMatchPattern_ExactMatch(t *testing.T) {
	if !matchPattern("workflow.orders.created", "workflow.orders.created") {
		t.Error("expected exact match to succeed")
	}
}

func TestMatchPattern_ExactNoMatch(t *testing.T) {
	if matchPattern("workflow.orders.created", "workflow.orders.deleted") {
		t.Error("expected exact non-match to fail")
	}
}

func TestMatchPattern_SingleWildcard_Matches(t *testing.T) {
	if !matchPattern("workflow.orders.*", "workflow.orders.created") {
		t.Error("expected single wildcard to match one segment")
	}
}

func TestMatchPattern_SingleWildcard_TooDeep(t *testing.T) {
	if matchPattern("workflow.orders.*", "workflow.orders.created.v2") {
		t.Error("expected single wildcard not to match multi-segment")
	}
}

func TestMatchPattern_DoubleWildcard_MatchesAll(t *testing.T) {
	if !matchPattern("workflow.**", "workflow.orders.created") {
		t.Error("expected ** to match deep path")
	}
}

func TestMatchPattern_DoubleWildcard_MatchesDeep(t *testing.T) {
	if !matchPattern("workflow.**", "workflow.x.y.z.w") {
		t.Error("expected ** to match deeply nested path")
	}
}

func TestMatchPattern_DoubleWildcard_AtEnd(t *testing.T) {
	if !matchPattern("**", "anything.at.all") {
		t.Error("expected standalone ** to match everything")
	}
}

func TestMatchPattern_DoubleWildcard_InMiddle(t *testing.T) {
	if !matchPattern("workflow.**.completed", "workflow.orders.processing.completed") {
		t.Error("expected ** in middle to match variable depth")
	}
}

func TestMatchPattern_NoMatch(t *testing.T) {
	if matchPattern("workflow.orders.*", "system.health.check") {
		t.Error("expected no match on different prefix")
	}
}

func TestMatchPattern_EmptyPattern(t *testing.T) {
	// Empty pattern "" vs empty event "" -> single empty segment matches single empty segment
	if !matchPattern("", "") {
		t.Error("expected empty to match empty")
	}
	// Empty pattern vs non-empty event
	if matchPattern("", "workflow.orders") {
		t.Error("expected empty pattern not to match non-empty event")
	}
}

func TestMatchPattern_SingleSegment(t *testing.T) {
	if !matchPattern("workflow", "workflow") {
		t.Error("expected single segment exact match")
	}
	if matchPattern("workflow", "system") {
		t.Error("expected single segment non-match")
	}
}

func TestMatchPattern_MultipleWildcards(t *testing.T) {
	if !matchPattern("*.orders.*", "workflow.orders.created") {
		t.Error("expected double single-wildcard to match")
	}
	if matchPattern("*.orders.*", "workflow.shipping.created") {
		t.Error("expected non-matching middle segment to fail")
	}
}

// --- CrossWorkflowRouter tests ---

type mockTriggerWorkflower struct {
	mu    sync.Mutex
	calls []triggerCall
}

type triggerCall struct {
	workflowType string
	action       string
	data         map[string]any
}

func (m *mockTriggerWorkflower) TriggerWorkflow(_ context.Context, workflowType string, action string, data map[string]any) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, triggerCall{workflowType, action, data})
	return nil
}

type mockManagedEngine struct {
	tw *mockTriggerWorkflower
}

func (m *mockManagedEngine) GetEngine() TriggerWorkflower {
	return m.tw
}

// nonTriggerableEngine does not implement triggerableEngine
type nonTriggerableEngine struct{}

type routerTestLinkStore struct {
	links []*store.CrossWorkflowLink
	err   error
}

func (s *routerTestLinkStore) Create(_ context.Context, l *store.CrossWorkflowLink) error {
	s.links = append(s.links, l)
	return nil
}

func (s *routerTestLinkStore) Get(_ context.Context, id uuid.UUID) (*store.CrossWorkflowLink, error) {
	for _, l := range s.links {
		if l.ID == id {
			return l, nil
		}
	}
	return nil, store.ErrNotFound
}

func (s *routerTestLinkStore) Delete(_ context.Context, _ uuid.UUID) error { return nil }

func (s *routerTestLinkStore) List(_ context.Context, _ store.CrossWorkflowLinkFilter) ([]*store.CrossWorkflowLink, error) {
	return s.links, s.err
}

func testRouterLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestCrossWorkflowRouter_RefreshLinks_Success(t *testing.T) {
	ls := &routerTestLinkStore{
		links: []*store.CrossWorkflowLink{
			{ID: uuid.New(), SourceWorkflowID: uuid.New(), TargetWorkflowID: uuid.New(), LinkType: "workflow.orders.*"},
		},
	}
	r := NewCrossWorkflowRouter(ls, func(_ uuid.UUID) (any, bool) { return nil, false }, testRouterLogger())

	if err := r.RefreshLinks(context.Background()); err != nil {
		t.Fatalf("refresh failed: %v", err)
	}
	if len(r.links) != 1 {
		t.Errorf("expected 1 link, got %d", len(r.links))
	}
}

func TestCrossWorkflowRouter_RefreshLinks_Empty(t *testing.T) {
	ls := &routerTestLinkStore{}
	r := NewCrossWorkflowRouter(ls, func(_ uuid.UUID) (any, bool) { return nil, false }, testRouterLogger())

	if err := r.RefreshLinks(context.Background()); err != nil {
		t.Fatalf("refresh failed: %v", err)
	}
	if len(r.links) != 0 {
		t.Errorf("expected 0 links, got %d", len(r.links))
	}
}

func TestCrossWorkflowRouter_RouteEvent_Match(t *testing.T) {
	sourceID := uuid.New()
	targetID := uuid.New()
	tw := &mockTriggerWorkflower{}
	me := &mockManagedEngine{tw: tw}

	ls := &routerTestLinkStore{
		links: []*store.CrossWorkflowLink{
			{ID: uuid.New(), SourceWorkflowID: sourceID, TargetWorkflowID: targetID, LinkType: "workflow.orders.*"},
		},
	}

	r := NewCrossWorkflowRouter(ls, func(id uuid.UUID) (any, bool) {
		if id == targetID {
			return me, true
		}
		return nil, false
	}, testRouterLogger())

	_ = r.RefreshLinks(context.Background())

	err := r.RouteEvent(context.Background(), sourceID, "workflow.orders.created", map[string]any{"key": "val"})
	if err != nil {
		t.Fatalf("route failed: %v", err)
	}
	if len(tw.calls) != 1 {
		t.Fatalf("expected 1 trigger call, got %d", len(tw.calls))
	}
	if tw.calls[0].workflowType != "workflow.orders.created" {
		t.Errorf("expected event type workflow.orders.created, got %s", tw.calls[0].workflowType)
	}
}

func TestCrossWorkflowRouter_RouteEvent_NoMatch(t *testing.T) {
	sourceID := uuid.New()
	targetID := uuid.New()
	tw := &mockTriggerWorkflower{}
	me := &mockManagedEngine{tw: tw}

	ls := &routerTestLinkStore{
		links: []*store.CrossWorkflowLink{
			{ID: uuid.New(), SourceWorkflowID: sourceID, TargetWorkflowID: targetID, LinkType: "workflow.orders.*"},
		},
	}

	r := NewCrossWorkflowRouter(ls, func(id uuid.UUID) (any, bool) {
		if id == targetID {
			return me, true
		}
		return nil, false
	}, testRouterLogger())

	_ = r.RefreshLinks(context.Background())

	_ = r.RouteEvent(context.Background(), sourceID, "system.health.check", nil)
	if len(tw.calls) != 0 {
		t.Errorf("expected 0 trigger calls for non-matching event, got %d", len(tw.calls))
	}
}

func TestCrossWorkflowRouter_RouteEvent_MultipleTargets(t *testing.T) {
	sourceID := uuid.New()
	target1ID := uuid.New()
	target2ID := uuid.New()
	tw1 := &mockTriggerWorkflower{}
	tw2 := &mockTriggerWorkflower{}
	me1 := &mockManagedEngine{tw: tw1}
	me2 := &mockManagedEngine{tw: tw2}

	ls := &routerTestLinkStore{
		links: []*store.CrossWorkflowLink{
			{ID: uuid.New(), SourceWorkflowID: sourceID, TargetWorkflowID: target1ID, LinkType: "workflow.**"},
			{ID: uuid.New(), SourceWorkflowID: sourceID, TargetWorkflowID: target2ID, LinkType: "workflow.**"},
		},
	}

	r := NewCrossWorkflowRouter(ls, func(id uuid.UUID) (any, bool) {
		switch id {
		case target1ID:
			return me1, true
		case target2ID:
			return me2, true
		}
		return nil, false
	}, testRouterLogger())

	_ = r.RefreshLinks(context.Background())
	_ = r.RouteEvent(context.Background(), sourceID, "workflow.orders.created", nil)

	if len(tw1.calls) != 1 {
		t.Errorf("expected 1 call to target1, got %d", len(tw1.calls))
	}
	if len(tw2.calls) != 1 {
		t.Errorf("expected 1 call to target2, got %d", len(tw2.calls))
	}
}

func TestCrossWorkflowRouter_RouteEvent_TargetNotRunning(t *testing.T) {
	sourceID := uuid.New()
	targetID := uuid.New()

	ls := &routerTestLinkStore{
		links: []*store.CrossWorkflowLink{
			{ID: uuid.New(), SourceWorkflowID: sourceID, TargetWorkflowID: targetID, LinkType: "workflow.**"},
		},
	}

	r := NewCrossWorkflowRouter(ls, func(_ uuid.UUID) (any, bool) {
		return nil, false // target not running
	}, testRouterLogger())

	_ = r.RefreshLinks(context.Background())
	err := r.RouteEvent(context.Background(), sourceID, "workflow.orders.created", nil)
	if err != nil {
		t.Fatalf("route should not error when target is missing, got %v", err)
	}
}

func TestCrossWorkflowRouter_RouteEvent_NonTriggerableEngine(t *testing.T) {
	sourceID := uuid.New()
	targetID := uuid.New()

	ls := &routerTestLinkStore{
		links: []*store.CrossWorkflowLink{
			{ID: uuid.New(), SourceWorkflowID: sourceID, TargetWorkflowID: targetID, LinkType: "workflow.**"},
		},
	}

	// Return a value that does NOT implement triggerableEngine
	r := NewCrossWorkflowRouter(ls, func(id uuid.UUID) (any, bool) {
		if id == targetID {
			return &nonTriggerableEngine{}, true
		}
		return nil, false
	}, testRouterLogger())

	_ = r.RefreshLinks(context.Background())
	err := r.RouteEvent(context.Background(), sourceID, "workflow.orders.created", nil)
	if err != nil {
		t.Fatalf("route should not error for non-triggerable engine, got %v", err)
	}
}

func TestCrossWorkflowRouter_RouteEvent_NoLinks(t *testing.T) {
	sourceID := uuid.New()
	ls := &routerTestLinkStore{}

	r := NewCrossWorkflowRouter(ls, func(_ uuid.UUID) (any, bool) { return nil, false }, testRouterLogger())
	_ = r.RefreshLinks(context.Background())

	err := r.RouteEvent(context.Background(), sourceID, "workflow.orders.created", nil)
	if err != nil {
		t.Fatalf("route with no links should succeed, got %v", err)
	}
}

func TestCrossWorkflowRouter_RouteEvent_DifferentSource(t *testing.T) {
	sourceID := uuid.New()
	otherSourceID := uuid.New()
	targetID := uuid.New()
	tw := &mockTriggerWorkflower{}
	me := &mockManagedEngine{tw: tw}

	ls := &routerTestLinkStore{
		links: []*store.CrossWorkflowLink{
			{ID: uuid.New(), SourceWorkflowID: otherSourceID, TargetWorkflowID: targetID, LinkType: "workflow.**"},
		},
	}

	r := NewCrossWorkflowRouter(ls, func(id uuid.UUID) (any, bool) {
		if id == targetID {
			return me, true
		}
		return nil, false
	}, testRouterLogger())

	_ = r.RefreshLinks(context.Background())
	_ = r.RouteEvent(context.Background(), sourceID, "workflow.orders.created", nil)

	if len(tw.calls) != 0 {
		t.Errorf("expected 0 calls when source doesn't match, got %d", len(tw.calls))
	}
}

func TestCrossWorkflowRouter_ConcurrentRouting(t *testing.T) {
	sourceID := uuid.New()
	targetID := uuid.New()
	tw := &mockTriggerWorkflower{}
	me := &mockManagedEngine{tw: tw}

	ls := &routerTestLinkStore{
		links: []*store.CrossWorkflowLink{
			{ID: uuid.New(), SourceWorkflowID: sourceID, TargetWorkflowID: targetID, LinkType: "workflow.**"},
		},
	}

	r := NewCrossWorkflowRouter(ls, func(id uuid.UUID) (any, bool) {
		if id == targetID {
			return me, true
		}
		return nil, false
	}, testRouterLogger())

	_ = r.RefreshLinks(context.Background())

	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			_ = r.RouteEvent(context.Background(), sourceID, "workflow.orders.created", nil)
		})
	}
	wg.Wait()

	tw.mu.Lock()
	defer tw.mu.Unlock()
	if len(tw.calls) != 20 {
		t.Errorf("expected 20 concurrent trigger calls, got %d", len(tw.calls))
	}
}
