package module

import (
	"errors"

	"github.com/GoCodeAlone/modular"
)

// EventProcessorLocator helps locate the event processor
type EventProcessorLocator struct {
	App modular.Application
}

// NewEventProcessorLocator creates a new locator
func NewEventProcessorLocator(app modular.Application) *EventProcessorLocator {
	return &EventProcessorLocator{App: app}
}

// Locate finds an event processor by name
func (l *EventProcessorLocator) Locate(name string) (*EventProcessor, error) {
	var processor *EventProcessor
	err := l.App.GetService(name, &processor)
	if err != nil || processor == nil {
		return nil, errors.New("event processor not found")
	}
	return processor, nil
}

// LocateDefault finds the default event processor
func (l *EventProcessorLocator) LocateDefault() (*EventProcessor, error) {
	// Try common names
	names := []string{"eventProcessor", "EventProcessor", "events"}

	for _, name := range names {
		if processor, err := l.Locate(name); err == nil {
			return processor, nil
		}
	}

	return nil, errors.New("no default event processor found")
}

// GetProcessor is a utility to get an event processor from the app
func GetProcessor(app modular.Application) (*EventProcessor, error) {
	locator := NewEventProcessorLocator(app)
	return locator.LocateDefault()
}
