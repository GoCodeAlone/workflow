package mock

import (
	"github.com/GoCodeAlone/modular"
)

// ApplicationWithReset extends the standard application with testing utilities
type ApplicationWithReset struct {
	modular.Application
}

// NewTestApplication creates a new application instance suitable for testing
func NewTestApplication() *ApplicationWithReset {
	logger := &Logger{LogEntries: make([]string, 0)}
	app := modular.NewStdApplication(modular.NewStdConfigProvider(nil), logger)
	return &ApplicationWithReset{Application: app}
}

// ResetServices clears any registered services for clean testing
func (a *ApplicationWithReset) ResetServices() {
	// This is a mock implementation that doesn't actually reset services
	// but in a real implementation, it would clear the service registry
}
