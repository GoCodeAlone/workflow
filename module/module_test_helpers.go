package module

import (
	"testing"

	"github.com/GoCodeAlone/modular"
)

// TestHelper provides utilities for module tests
type TestHelper struct {
	app modular.Application
}

// NewTestHelper creates a new test helper
func NewTestHelper(app modular.Application) *TestHelper {
	return &TestHelper{app: app}
}

// CreateIsolatedApp creates an isolated application for tests
func CreateIsolatedApp(t *testing.T) modular.Application {
	t.Helper()

	// Use standard test logger - don't import from mock to avoid cycles
	logger := &testLogger{entries: make([]string, 0)}

	// Create a clean application
	app := modular.NewStdApplication(modular.NewStdConfigProvider(nil), logger)

	// Initialize
	err := app.Init()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}

	return app
}

// Simple test logger implementation to avoid importing from mock
type testLogger struct {
	entries []string
}

func (l *testLogger) Debug(msg string, args ...interface{}) {}
func (l *testLogger) Info(msg string, args ...interface{})  {}
func (l *testLogger) Warn(msg string, args ...interface{})  {}
func (l *testLogger) Error(msg string, args ...interface{}) {}
func (l *testLogger) Fatal(msg string, args ...interface{}) {}
