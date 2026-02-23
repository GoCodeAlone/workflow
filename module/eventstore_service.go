package module

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/CrisisTextLine/modular"
	evstore "github.com/GoCodeAlone/workflow/store"
)

// EventStoreServiceConfig holds the configuration for the event store service module.
type EventStoreServiceConfig struct {
	DBPath string `yaml:"db_path" default:"data/events.db"`
	// RetentionDays is reserved for future implementation of automatic event pruning.
	// It is stored and exposed via RetentionDays() but not yet applied to the store.
	RetentionDays int `yaml:"retention_days" default:"90"`
}

// EventStoreServiceModule wraps an evstore.SQLiteEventStore as a modular.Module.
// It initializes the store and makes it available in the modular service registry.
type EventStoreServiceModule struct {
	name   string
	config EventStoreServiceConfig
	store  *evstore.SQLiteEventStore
}

// NewEventStoreServiceModule creates a new event store service module with the given name and config.
func NewEventStoreServiceModule(name string, cfg EventStoreServiceConfig) (*EventStoreServiceModule, error) {
	dbPath := cfg.DBPath
	if dbPath == "" {
		dbPath = "data/events.db"
	}

	// Ensure parent directory exists for the SQLite file.
	if dir := filepath.Dir(dbPath); dir != "" && dir != "." {
		if mkErr := os.MkdirAll(dir, 0o750); mkErr != nil {
			return nil, fmt.Errorf("eventstore.service %q: failed to create db directory %q: %w", name, dir, mkErr)
		}
	}

	store, err := evstore.NewSQLiteEventStore(dbPath)
	if err != nil {
		return nil, fmt.Errorf("eventstore.service %q: failed to open store: %w", name, err)
	}

	slog.Default().Info("Opened event store", "module", name, "path", dbPath)

	return &EventStoreServiceModule{
		name:   name,
		config: cfg,
		store:  store,
	}, nil
}

// Name implements modular.Module.
func (m *EventStoreServiceModule) Name() string { return m.name }

// Init implements modular.Module.
func (m *EventStoreServiceModule) Init(_ modular.Application) error { return nil }

// ProvidesServices implements modular.Module. The event store is registered under
// the module name so other modules (timeline, replay, DLQ) can look it up.
func (m *EventStoreServiceModule) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{
			Name:        m.name,
			Description: "Event store service: " + m.name,
			Instance:    m.store,
		},
		{
			Name:        m.name + ".admin",
			Description: "Event store admin (closable): " + m.name,
			Instance:    m.store,
		},
	}
}

// RequiresServices implements modular.Module.
func (m *EventStoreServiceModule) RequiresServices() []modular.ServiceDependency {
	return nil
}

// Store returns the underlying SQLiteEventStore for direct use.
func (m *EventStoreServiceModule) Store() *evstore.SQLiteEventStore {
	return m.store
}

// RetentionDays returns the configured retention period.
func (m *EventStoreServiceModule) RetentionDays() int {
	return m.config.RetentionDays
}
