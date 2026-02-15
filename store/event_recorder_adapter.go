package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// EventRecorderAdapter wraps an EventStore to satisfy the
// module.EventRecorder interface used by pipeline execution.
// The adapter converts string execution IDs to uuid.UUID before
// delegating to the underlying EventStore.Append method.
type EventRecorderAdapter struct {
	store EventStore
}

// NewEventRecorderAdapter creates an adapter that bridges
// EventStore to module.EventRecorder.
func NewEventRecorderAdapter(store EventStore) *EventRecorderAdapter {
	return &EventRecorderAdapter{store: store}
}

// RecordEvent implements module.EventRecorder.
func (a *EventRecorderAdapter) RecordEvent(ctx context.Context, executionID string, eventType string, data map[string]any) error {
	id, err := uuid.Parse(executionID)
	if err != nil {
		return fmt.Errorf("parse execution ID %q: %w", executionID, err)
	}
	return a.store.Append(ctx, id, eventType, data)
}
