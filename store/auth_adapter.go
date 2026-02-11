package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/CrisisTextLine/modular/modules/auth"
	"github.com/google/uuid"
)

// AuthUserStoreAdapter adapts a store.UserStore to the auth.UserStore interface.
type AuthUserStoreAdapter struct {
	store UserStore
}

// NewAuthUserStoreAdapter creates a new adapter.
func NewAuthUserStoreAdapter(store UserStore) *AuthUserStoreAdapter {
	return &AuthUserStoreAdapter{store: store}
}

func (a *AuthUserStoreAdapter) GetUser(ctx context.Context, userID string) (*auth.User, error) {
	id, err := uuid.Parse(userID)
	if err != nil {
		return nil, auth.ErrUserNotFound
	}
	u, err := a.store.Get(ctx, id)
	if err != nil {
		return nil, auth.ErrUserNotFound
	}
	return toAuthUser(u), nil
}

func (a *AuthUserStoreAdapter) GetUserByEmail(ctx context.Context, email string) (*auth.User, error) {
	u, err := a.store.GetByEmail(ctx, email)
	if err != nil {
		return nil, auth.ErrUserNotFound
	}
	return toAuthUser(u), nil
}

func (a *AuthUserStoreAdapter) CreateUser(ctx context.Context, user *auth.User) error {
	u := fromAuthUser(user)
	if err := a.store.Create(ctx, u); err != nil {
		if isStoreDuplicate(err) {
			return auth.ErrUserAlreadyExists
		}
		return err
	}
	user.ID = u.ID.String()
	user.CreatedAt = u.CreatedAt
	user.UpdatedAt = u.UpdatedAt
	return nil
}

func (a *AuthUserStoreAdapter) UpdateUser(ctx context.Context, user *auth.User) error {
	u := fromAuthUser(user)
	if err := a.store.Update(ctx, u); err != nil {
		if isStoreNotFound(err) {
			return auth.ErrUserNotFound
		}
		return err
	}
	return nil
}

func (a *AuthUserStoreAdapter) DeleteUser(ctx context.Context, userID string) error {
	id, err := uuid.Parse(userID)
	if err != nil {
		return auth.ErrUserNotFound
	}
	if err := a.store.Delete(ctx, id); err != nil {
		if isStoreNotFound(err) {
			return auth.ErrUserNotFound
		}
		return err
	}
	return nil
}

// AuthSessionStoreAdapter adapts a store.SessionStore to the auth.SessionStore interface.
type AuthSessionStoreAdapter struct {
	store SessionStore
}

// NewAuthSessionStoreAdapter creates a new adapter.
func NewAuthSessionStoreAdapter(store SessionStore) *AuthSessionStoreAdapter {
	return &AuthSessionStoreAdapter{store: store}
}

func (a *AuthSessionStoreAdapter) Store(ctx context.Context, session *auth.Session) error {
	s := fromAuthSession(session)
	return a.store.Create(ctx, s)
}

func (a *AuthSessionStoreAdapter) Get(ctx context.Context, sessionID string) (*auth.Session, error) {
	id, err := uuid.Parse(sessionID)
	if err != nil {
		return nil, auth.ErrSessionNotFound
	}
	s, err := a.store.Get(ctx, id)
	if err != nil {
		return nil, auth.ErrSessionNotFound
	}
	return toAuthSession(s), nil
}

func (a *AuthSessionStoreAdapter) Delete(ctx context.Context, sessionID string) error {
	id, err := uuid.Parse(sessionID)
	if err != nil {
		return auth.ErrSessionNotFound
	}
	return a.store.Delete(ctx, id)
}

func (a *AuthSessionStoreAdapter) Cleanup(ctx context.Context) error {
	_, err := a.store.DeleteExpired(ctx)
	return err
}

// --- conversion helpers ---

func toAuthUser(u *User) *auth.User {
	au := &auth.User{
		ID:           u.ID.String(),
		Email:        u.Email,
		PasswordHash: u.PasswordHash,
		Active:       u.Active,
		CreatedAt:    u.CreatedAt,
		UpdatedAt:    u.UpdatedAt,
		LastLoginAt:  u.LastLoginAt,
	}
	if len(u.Metadata) > 0 {
		_ = json.Unmarshal(u.Metadata, &au.Metadata)
	}
	return au
}

func fromAuthUser(au *auth.User) *User {
	u := &User{
		Email:        au.Email,
		PasswordHash: au.PasswordHash,
		Active:       au.Active,
		CreatedAt:    au.CreatedAt,
		UpdatedAt:    au.UpdatedAt,
		LastLoginAt:  au.LastLoginAt,
	}
	if au.ID != "" {
		id, err := uuid.Parse(au.ID)
		if err == nil {
			u.ID = id
		}
	}
	if au.Metadata != nil {
		u.Metadata, _ = json.Marshal(au.Metadata)
	}
	return u
}

func toAuthSession(s *Session) *auth.Session {
	as := &auth.Session{
		ID:        s.ID.String(),
		UserID:    s.UserID.String(),
		CreatedAt: s.CreatedAt,
		ExpiresAt: s.ExpiresAt,
		IPAddress: s.IPAddress,
		UserAgent: s.UserAgent,
		Active:    s.Active,
	}
	if len(s.Metadata) > 0 {
		_ = json.Unmarshal(s.Metadata, &as.Metadata)
	}
	return as
}

func fromAuthSession(as *auth.Session) *Session {
	s := &Session{
		IPAddress: as.IPAddress,
		UserAgent: as.UserAgent,
		Active:    as.Active,
		ExpiresAt: as.ExpiresAt,
		CreatedAt: as.CreatedAt,
	}
	if as.ID != "" {
		id, err := uuid.Parse(as.ID)
		if err == nil {
			s.ID = id
		}
	}
	if as.UserID != "" {
		uid, err := uuid.Parse(as.UserID)
		if err == nil {
			s.UserID = uid
		}
	}
	if s.CreatedAt.IsZero() {
		s.CreatedAt = time.Now()
	}
	if as.Metadata != nil {
		s.Metadata, _ = json.Marshal(as.Metadata)
	}
	return s
}

func isStoreDuplicate(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, ErrDuplicate)
}

func isStoreNotFound(err error) bool {
	return err == ErrNotFound
}
