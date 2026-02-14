package module

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/GoCodeAlone/workflow/store"
	"github.com/google/uuid"
)

// engineGetter is the function signature for retrieving a managed engine by ID.
// It returns a pointer to the engine wrapper and whether it was found.
// The concrete type is *workflow.ManagedEngine but we use interface{} here to
// avoid a circular import between the workflow and module packages.
type engineGetter func(uuid.UUID) (any, bool)

// triggerableEngine is implemented by any engine wrapper whose Engine field
// exposes TriggerWorkflow. We use duck-typing via reflection-free interface
// assertion to call it.
type triggerableEngine interface {
	GetEngine() TriggerWorkflower
}

// TriggerWorkflower is the subset of the engine interface needed for routing.
type TriggerWorkflower interface {
	TriggerWorkflow(ctx context.Context, workflowType string, action string, data map[string]any) error
}

// CrossWorkflowRouter routes events from one workflow to linked target workflows.
type CrossWorkflowRouter struct {
	mu        sync.RWMutex
	links     []*store.CrossWorkflowLink
	linkStore store.CrossWorkflowLinkStore
	getEngine engineGetter
	logger    *slog.Logger
}

// NewCrossWorkflowRouter creates a new router. The getEngine callback must return
// a value whose concrete type has a field or method that provides a TriggerWorkflower.
// In practice this is *workflow.ManagedEngine.
func NewCrossWorkflowRouter(linkStore store.CrossWorkflowLinkStore, getEngine func(uuid.UUID) (any, bool), logger *slog.Logger) *CrossWorkflowRouter {
	return &CrossWorkflowRouter{
		linkStore: linkStore,
		getEngine: getEngine,
		logger:    logger,
	}
}

// RefreshLinks reloads link configurations from the database.
func (r *CrossWorkflowRouter) RefreshLinks(ctx context.Context) error {
	links, err := r.linkStore.List(ctx, store.CrossWorkflowLinkFilter{
		Pagination: store.Pagination{Offset: 0, Limit: 1000},
	})
	if err != nil {
		return fmt.Errorf("failed to load cross-workflow links: %w", err)
	}

	r.mu.Lock()
	r.links = links
	r.mu.Unlock()

	r.logger.Info("Refreshed cross-workflow links", "count", len(links))
	return nil
}

// RouteEvent checks if an event from a source workflow should be forwarded
// to any target workflows based on configured links.
func (r *CrossWorkflowRouter) RouteEvent(ctx context.Context, sourceWorkflowID uuid.UUID, eventType string, eventData any) error {
	r.mu.RLock()
	links := r.links
	r.mu.RUnlock()

	for _, link := range links {
		if link.SourceWorkflowID != sourceWorkflowID {
			continue
		}

		// Use link_type as the event pattern to match against
		if !matchPattern(link.LinkType, eventType) {
			continue
		}

		r.logger.Info("Routing event",
			"source", sourceWorkflowID,
			"target", link.TargetWorkflowID,
			"event_type", eventType,
		)

		// Get the target engine
		engineIface, ok := r.getEngine(link.TargetWorkflowID)
		if !ok {
			r.logger.Warn("Target workflow not running",
				"target", link.TargetWorkflowID,
				"event_type", eventType,
			)
			continue
		}

		// Try to trigger the workflow via duck-typing
		if te, ok := engineIface.(triggerableEngine); ok {
			data := map[string]any{
				"source_workflow_id": sourceWorkflowID.String(),
				"event_type":         eventType,
				"event_data":         eventData,
			}
			if err := te.GetEngine().TriggerWorkflow(ctx, eventType, "cross-workflow", data); err != nil {
				r.logger.Error("Failed to route event to target workflow",
					"target", link.TargetWorkflowID,
					"event_type", eventType,
					"error", err,
				)
			}
		} else {
			r.logger.Warn("Target engine does not support TriggerWorkflow",
				"target", link.TargetWorkflowID,
			)
		}
	}

	return nil
}

// matchPattern performs glob-style matching on dotted event type strings.
// Supported wildcards:
//   - "*"  matches a single segment (between dots)
//   - "**" matches zero or more segments
//
// Examples:
//
//	"workflow.orders.*"  matches "workflow.orders.created"
//	"workflow.**"        matches "workflow.orders.created" and "workflow.x.y.z"
//	"*"                  matches any single-segment string
func matchPattern(pattern, eventType string) bool {
	patternParts := strings.Split(pattern, ".")
	eventParts := strings.Split(eventType, ".")
	return matchParts(patternParts, eventParts)
}

func matchParts(pattern, event []string) bool {
	pi, ei := 0, 0
	for pi < len(pattern) && ei < len(event) {
		switch pattern[pi] {
		case "**":
			// ** at end of pattern matches everything remaining
			if pi == len(pattern)-1 {
				return true
			}
			// Try matching rest of pattern at every possible position
			for k := ei; k <= len(event); k++ {
				if matchParts(pattern[pi+1:], event[k:]) {
					return true
				}
			}
			return false
		case "*":
			// * matches exactly one segment
			pi++
			ei++
		default:
			if pattern[pi] != event[ei] {
				return false
			}
			pi++
			ei++
		}
	}

	// Consume any trailing ** in pattern
	for pi < len(pattern) && pattern[pi] == "**" {
		pi++
	}

	return pi == len(pattern) && ei == len(event)
}
