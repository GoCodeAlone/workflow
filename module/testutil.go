package module

import (
	"context"

	"github.com/CrisisTextLine/modular"
)

// TestLogger is a simple logger for testing
type TestLogger struct {
	Entries []string
}

func (l *TestLogger) Debug(msg string, args ...interface{}) {}
func (l *TestLogger) Info(msg string, args ...interface{})  {}
func (l *TestLogger) Warn(msg string, args ...interface{})  {}
func (l *TestLogger) Error(msg string, args ...interface{}) {}
func (l *TestLogger) Fatal(msg string, args ...interface{}) {}

// NewTestApplication creates an isolated test application
func NewTestApplication() (modular.Application, *TestLogger) {
	logger := &TestLogger{Entries: make([]string, 0)}
	app := modular.NewStdApplication(modular.NewStdConfigProvider(nil), logger)
	return app, logger
}

// Skip tests with a context.Context parameter
func SkipTestWithContext(ctx context.Context, skip bool) context.Context {
	if skip {
		return ctx
	}
	return ctx
}
