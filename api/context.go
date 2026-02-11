package api

import (
	"context"

	"github.com/GoCodeAlone/workflow/store"
	"github.com/google/uuid"
)

type contextKey int

const (
	contextKeyUser contextKey = iota
	contextKeyRequestID
)

// SetUserContext returns a new context with the user attached.
func SetUserContext(ctx context.Context, u *store.User) context.Context {
	return context.WithValue(ctx, contextKeyUser, u)
}

// UserFromContext extracts the authenticated user from context, or nil.
func UserFromContext(ctx context.Context) *store.User {
	u, _ := ctx.Value(contextKeyUser).(*store.User)
	return u
}

// SetRequestID returns a new context with the request ID attached.
func SetRequestID(ctx context.Context, id uuid.UUID) context.Context {
	return context.WithValue(ctx, contextKeyRequestID, id)
}

// RequestIDFromContext extracts the request ID from context.
func RequestIDFromContext(ctx context.Context) uuid.UUID {
	id, _ := ctx.Value(contextKeyRequestID).(uuid.UUID)
	return id
}
