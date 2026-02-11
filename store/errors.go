package store

import "errors"

// Sentinel errors for store operations.
var (
	ErrNotFound  = errors.New("not found")
	ErrDuplicate = errors.New("duplicate entry")
	ErrConflict  = errors.New("conflict")
	ErrForbidden = errors.New("forbidden")
)
