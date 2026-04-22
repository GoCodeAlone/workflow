package interfaces

import (
	"context"
	"fmt"
	"time"
)

// MigrationDriver is the interface that migration backend plugins implement.
// Implementations live in workflow-plugin-migrations (golang-migrate, goose, atlas).
type MigrationDriver interface {
	Name() string
	Up(ctx context.Context, req MigrationRequest) (MigrationResult, error)
	Down(ctx context.Context, req MigrationRequest) (MigrationResult, error)
	Status(ctx context.Context, req MigrationRequest) (MigrationStatus, error)
	Goto(ctx context.Context, req MigrationRequest, target string) (MigrationResult, error)
}

// MigrationRequest holds all parameters needed to run a migration operation.
type MigrationRequest struct {
	DSN     string
	Source  MigrationSource
	Options MigrationOptions
}

// Validate returns ErrValidation if required fields are missing.
func (r MigrationRequest) Validate() error {
	if r.DSN == "" {
		return fmt.Errorf("%w: DSN is required", ErrValidation)
	}
	if r.Source.Dir == "" && len(r.Source.Files) == 0 {
		return fmt.Errorf("%w: source (dir or files) required", ErrValidation)
	}
	return nil
}

// MigrationSource describes where migration files come from.
type MigrationSource struct {
	Dir        string
	Files      []string
	SchemaName string
}

// MigrationOptions controls migration behaviour.
type MigrationOptions struct {
	Steps   int
	DryRun  bool
	Timeout time.Duration
	Version string
}

// MigrationResult is returned after a successful migration operation.
type MigrationResult struct {
	Applied    []string
	Skipped    []string
	DurationMs int64
}

// MigrationStatus describes the current migration state of a database.
type MigrationStatus struct {
	Current string
	Pending []string
	Dirty   bool
}
