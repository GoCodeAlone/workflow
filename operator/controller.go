package operator

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
)

// ControllerEvent represents a watch event for a WorkflowDefinition.
type ControllerEvent struct {
	Type       string // "ADDED", "MODIFIED", "DELETED"
	Definition *WorkflowDefinition
}

// Event type constants.
const (
	EventAdded    = "ADDED"
	EventModified = "MODIFIED"
	EventDeleted  = "DELETED"
)

// Controller simulates a K8s controller watch loop.
// In production, this would use client-go's informer framework with
// shared informers and work queues. This implementation provides the
// same logical flow for testing and local development.
type Controller struct {
	reconciler *Reconciler
	queue      chan ControllerEvent
	logger     *slog.Logger
	cancel     context.CancelFunc
	mu         sync.Mutex
	running    bool
	done       chan struct{}
}

// NewController creates a new Controller backed by the given Reconciler.
func NewController(reconciler *Reconciler, logger *slog.Logger) *Controller {
	if logger == nil {
		logger = slog.Default()
	}
	return &Controller{
		reconciler: reconciler,
		queue:      make(chan ControllerEvent, 256),
		logger:     logger,
		done:       make(chan struct{}),
	}
}

// Start begins the controller's event processing loop. It blocks until the
// context is cancelled or Stop is called. The loop drains the event queue and
// dispatches each event to the reconciler.
func (c *Controller) Start(ctx context.Context) error {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return fmt.Errorf("controller is already running")
	}

	ctx, cancel := context.WithCancel(ctx)
	c.cancel = cancel
	c.running = true
	c.done = make(chan struct{})
	c.mu.Unlock()

	c.logger.Info("Controller started")

	defer func() {
		c.mu.Lock()
		c.running = false
		close(c.done)
		c.mu.Unlock()
		c.logger.Info("Controller stopped")
	}()

	for {
		select {
		case <-ctx.Done():
			return nil
		case event, ok := <-c.queue:
			if !ok {
				return nil
			}
			c.processEvent(ctx, event)
		}
	}
}

// Stop signals the controller to shut down and waits for the event loop
// to finish draining.
func (c *Controller) Stop() error {
	c.mu.Lock()
	if !c.running {
		c.mu.Unlock()
		return fmt.Errorf("controller is not running")
	}
	cancel := c.cancel
	done := c.done
	c.mu.Unlock()

	if cancel != nil {
		cancel()
	}

	// Wait for the event loop to exit.
	<-done
	return nil
}

// Enqueue adds an event to the controller's work queue. Events are processed
// in FIFO order by the event loop. If the queue is full the event is dropped
// and a warning is logged.
func (c *Controller) Enqueue(event ControllerEvent) {
	select {
	case c.queue <- event:
		c.logger.Debug("Enqueued event", "type", event.Type, "name", event.Definition.Metadata.Name)
	default:
		c.logger.Warn("Event queue full, dropping event", "type", event.Type, "name", event.Definition.Metadata.Name)
	}
}

// IsRunning returns whether the controller's event loop is active.
func (c *Controller) IsRunning() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.running
}

// processEvent dispatches a single event to the reconciler.
func (c *Controller) processEvent(ctx context.Context, event ControllerEvent) {
	c.logger.Info("Processing event", "type", event.Type, "name", event.Definition.Metadata.Name)

	switch event.Type {
	case EventAdded, EventModified:
		result, err := c.reconciler.Reconcile(ctx, event.Definition)
		if err != nil {
			c.logger.Error("Reconciliation failed",
				"type", event.Type,
				"name", event.Definition.Metadata.Name,
				"error", err,
			)
			return
		}
		c.logger.Info("Reconciliation complete",
			"type", event.Type,
			"name", event.Definition.Metadata.Name,
			"action", result.Action,
			"message", result.Message,
		)

	case EventDeleted:
		if err := c.reconciler.Delete(ctx, event.Definition.Metadata.Name, event.Definition.Metadata.Namespace); err != nil {
			c.logger.Error("Deletion failed",
				"name", event.Definition.Metadata.Name,
				"error", err,
			)
			return
		}
		c.logger.Info("Deletion complete", "name", event.Definition.Metadata.Name)

	default:
		c.logger.Warn("Unknown event type", "type", event.Type)
	}
}
