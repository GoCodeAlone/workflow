package module

import (
	"context"
	"fmt"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// Compile-time assertion: *WorkflowRegistry must satisfy interfaces.WorkflowStoreProvider.
var _ interfaces.WorkflowStoreProvider = (*WorkflowRegistry)(nil)

// WorkflowRegistry is a module that provides the V1Store as a service,
// making the workflow data store (companies, projects, workflows) available
// to other modules via the service registry. It can either use a shared
// SQLiteStorage backend (via storageBackend config) or open its own database.
type WorkflowRegistry struct {
	name           string
	storageBackend string
	store          *V1Store
	storage        *SQLiteStorage
	logger         modular.Logger
}

// NewWorkflowRegistry creates a new workflow registry module.
// If storageBackend is non-empty, it uses that SQLiteStorage service's DB;
// otherwise it opens its own database at the default path.
func NewWorkflowRegistry(name, storageBackend string) *WorkflowRegistry {
	return &WorkflowRegistry{
		name:           name,
		storageBackend: storageBackend,
		logger:         &noopLogger{},
	}
}

func (w *WorkflowRegistry) Name() string { return w.name }

func (w *WorkflowRegistry) Init(app modular.Application) error {
	w.logger = app.Logger()

	// Wire storage backend if configured
	if w.storageBackend != "" {
		var storage any
		if err := app.GetService(w.storageBackend, &storage); err == nil && storage != nil {
			if s, ok := storage.(*SQLiteStorage); ok {
				w.storage = s
			}
		}
	}

	return nil
}

// Start initializes the V1Store, using the shared storage backend or its own DB.
func (w *WorkflowRegistry) Start(_ context.Context) error {
	if w.storage != nil && w.storage.DB() != nil {
		// Use the shared database connection
		store := &V1Store{db: w.storage.DB()}
		if err := store.initSchema(); err != nil {
			return fmt.Errorf("init workflow registry schema: %w", err)
		}
		w.store = store
		w.logger.Info("WorkflowRegistry started with shared storage", "backend", w.storageBackend)
	} else {
		// Fallback: open a standalone database
		store, err := OpenV1Store("./data/workflow-registry.db")
		if err != nil {
			return fmt.Errorf("open workflow registry store: %w", err)
		}
		w.store = store
		w.logger.Info("WorkflowRegistry started with standalone storage")
	}
	return nil
}

// Stop closes the database if using standalone storage.
func (w *WorkflowRegistry) Stop(_ context.Context) error {
	// Only close if we own the database (not shared)
	if w.storage == nil && w.store != nil {
		return w.store.Close()
	}
	return nil
}

// Store returns the underlying V1Store.
func (w *WorkflowRegistry) Store() *V1Store {
	return w.store
}

// WorkflowStore satisfies the interfaces.WorkflowStoreProvider interface.
// It returns the underlying V1Store as an opaque any value so that the
// interfaces package does not need to import the module package.
func (w *WorkflowRegistry) WorkflowStore() any {
	return w.store
}

func (w *WorkflowRegistry) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{Name: w.name, Description: "Workflow data registry (companies, projects, workflows)", Instance: w},
	}
}

func (w *WorkflowRegistry) RequiresServices() []modular.ServiceDependency {
	if w.storageBackend != "" {
		return []modular.ServiceDependency{
			{Name: w.storageBackend, Required: false},
		}
	}
	return nil
}
