package module

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/CrisisTextLine/modular"
	"golang.org/x/crypto/bcrypt"
)

// UserStore provides user CRUD operations backed by an in-memory store
// with optional persistence write-through. It can be consumed by auth
// modules (e.g. auth.jwt) and management APIs.
type UserStore struct {
	name        string
	users       map[string]*User // keyed by email
	mu          sync.RWMutex
	nextID      int
	persistence *PersistenceStore
}

// NewUserStore creates a new user store module.
func NewUserStore(name string) *UserStore {
	return &UserStore{
		name:   name,
		users:  make(map[string]*User),
		nextID: 1,
	}
}

func (u *UserStore) Name() string { return u.name }

func (u *UserStore) Init(app modular.Application) error {
	// Wire optional persistence backend
	var ps any
	if err := app.GetService("persistence", &ps); err == nil && ps != nil {
		if store, ok := ps.(*PersistenceStore); ok {
			u.persistence = store
		}
	}

	// Load existing users from persistence if available
	if u.persistence != nil {
		u.loadFromPersistence()
	}

	return nil
}

// Start is a no-op.
func (u *UserStore) Start(_ context.Context) error {
	return nil
}

// Stop is a no-op.
func (u *UserStore) Stop(_ context.Context) error {
	return nil
}

func (u *UserStore) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{Name: u.name, Description: "User storage with CRUD operations", Instance: u},
	}
}

func (u *UserStore) RequiresServices() []modular.ServiceDependency {
	return []modular.ServiceDependency{
		{Name: "persistence", Required: false},
	}
}

// ListUsers returns all users.
func (u *UserStore) ListUsers() []*User {
	u.mu.RLock()
	defer u.mu.RUnlock()
	result := make([]*User, 0, len(u.users))
	for _, user := range u.users {
		result = append(result, user)
	}
	return result
}

// GetUser returns a user by email.
func (u *UserStore) GetUser(email string) (*User, bool) {
	u.mu.RLock()
	defer u.mu.RUnlock()
	user, ok := u.users[email]
	return user, ok
}

// GetUserByID returns a user by ID.
func (u *UserStore) GetUserByID(id string) (*User, bool) {
	u.mu.RLock()
	defer u.mu.RUnlock()
	for _, user := range u.users {
		if user.ID == id {
			return user, true
		}
	}
	return nil, false
}

// CreateUser creates a new user with the given email, name, and password.
func (u *UserStore) CreateUser(email, name, password string, metadata map[string]any) (*User, error) {
	u.mu.Lock()
	defer u.mu.Unlock()

	if _, exists := u.users[email]; exists {
		return nil, fmt.Errorf("user with email %q already exists", email)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	user := &User{
		ID:           fmt.Sprintf("user_%d", u.nextID),
		Email:        email,
		Name:         name,
		PasswordHash: string(hash),
		Metadata:     metadata,
		CreatedAt:    time.Now(),
	}
	u.nextID++
	u.users[email] = user

	u.persistUserLocked(user)
	return user, nil
}

// DeleteUser removes a user by ID.
func (u *UserStore) DeleteUser(id string) error {
	u.mu.Lock()
	defer u.mu.Unlock()

	for email, user := range u.users {
		if user.ID == id {
			delete(u.users, email)
			return nil
		}
	}
	return fmt.Errorf("user %q not found", id)
}

// UpdateUserMetadata updates the metadata for a user identified by ID.
func (u *UserStore) UpdateUserMetadata(id string, metadata map[string]any) error {
	u.mu.Lock()
	defer u.mu.Unlock()

	for _, user := range u.users {
		if user.ID == id {
			user.Metadata = metadata
			u.persistUserLocked(user)
			return nil
		}
	}
	return fmt.Errorf("user %q not found", id)
}

// VerifyPassword checks if the password matches the stored hash for the given email.
func (u *UserStore) VerifyPassword(email, password string) (*User, error) {
	u.mu.RLock()
	user, exists := u.users[email]
	u.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("user not found")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return nil, fmt.Errorf("invalid credentials")
	}

	return user, nil
}

// UserCount returns the number of users.
func (u *UserStore) UserCount() int {
	u.mu.RLock()
	defer u.mu.RUnlock()
	return len(u.users)
}

// LoadSeedFile loads users from a JSON file.
func (u *UserStore) LoadSeedFile(path string) error {
	if path == "" {
		return nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil // Silently skip missing seed files
	}

	var seeds []struct {
		Email    string `json:"email"`
		Name     string `json:"name"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if err := json.Unmarshal(data, &seeds); err != nil {
		return fmt.Errorf("parse seed file: %w", err)
	}

	for _, seed := range seeds {
		meta := map[string]any{}
		if seed.Role != "" {
			meta["role"] = seed.Role
		}
		if _, err := u.CreateUser(seed.Email, seed.Name, seed.Password, meta); err != nil {
			// Skip duplicate users
			continue
		}
	}
	return nil
}

func (u *UserStore) persistUserLocked(user *User) {
	if u.persistence == nil {
		return
	}
	_ = u.persistence.SaveUser(UserRecord{
		ID:           user.ID,
		Email:        user.Email,
		Name:         user.Name,
		PasswordHash: user.PasswordHash,
		Metadata:     user.Metadata,
		CreatedAt:    user.CreatedAt,
	})
}

func (u *UserStore) loadFromPersistence() {
	if u.persistence == nil {
		return
	}
	records, err := u.persistence.LoadUsers()
	if err != nil {
		return
	}
	for _, rec := range records {
		u.users[rec.Email] = &User{
			ID:           rec.ID,
			Email:        rec.Email,
			Name:         rec.Name,
			PasswordHash: rec.PasswordHash,
			Metadata:     rec.Metadata,
			CreatedAt:    rec.CreatedAt,
		}
		// Track highest ID for nextID
		var idNum int
		if _, err := fmt.Sscanf(rec.ID, "user_%d", &idNum); err == nil && idNum >= u.nextID {
			u.nextID = idNum + 1
		}
	}
}
