package module

import (
	"log/slog"
	"net/http"

	"github.com/CrisisTextLine/modular"
	evstore "github.com/GoCodeAlone/workflow/store"
)

// DLQServiceConfig holds the configuration for the DLQ service module.
type DLQServiceConfig struct {
	MaxRetries    int `yaml:"max_retries" default:"3"`
	RetentionDays int `yaml:"retention_days" default:"30"`
}

// DLQServiceModule wraps an evstore.DLQHandler as a modular.Module.
// It initializes the in-memory DLQ store and handler, making them available
// in the modular service registry.
type DLQServiceModule struct {
	name    string
	config  DLQServiceConfig
	store   *evstore.InMemoryDLQStore
	handler *evstore.DLQHandler
	mux     *http.ServeMux
}

// NewDLQServiceModule creates a new DLQ service module with the given name and config.
func NewDLQServiceModule(name string, cfg DLQServiceConfig) *DLQServiceModule {
	logger := slog.Default()

	dlqStore := evstore.NewInMemoryDLQStore()
	dlqHandler := evstore.NewDLQHandler(dlqStore, logger)
	dlqMux := http.NewServeMux()
	dlqHandler.RegisterRoutes(dlqMux)

	logger.Info("Created DLQ handler", "module", name)

	return &DLQServiceModule{
		name:    name,
		config:  cfg,
		store:   dlqStore,
		handler: dlqHandler,
		mux:     dlqMux,
	}
}

// Name implements modular.Module.
func (m *DLQServiceModule) Name() string { return m.name }

// Init implements modular.Module.
func (m *DLQServiceModule) Init(_ modular.Application) error { return nil }

// ProvidesServices implements modular.Module. The DLQ handler mux is registered
// under the module name and also under {name}.admin for admin route delegation.
func (m *DLQServiceModule) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{
			Name:        m.name,
			Description: "DLQ service: " + m.name,
			Instance:    m.mux,
		},
		{
			Name:        m.name + ".admin",
			Description: "DLQ admin handler: " + m.name,
			Instance:    http.Handler(m.mux),
		},
		{
			Name:        m.name + ".store",
			Description: "DLQ store: " + m.name,
			Instance:    m.store,
		},
	}
}

// RequiresServices implements modular.Module.
func (m *DLQServiceModule) RequiresServices() []modular.ServiceDependency {
	return nil
}

// DLQMux returns the HTTP mux for DLQ endpoints.
func (m *DLQServiceModule) DLQMux() http.Handler { return m.mux }

// Store returns the underlying DLQ store.
func (m *DLQServiceModule) Store() *evstore.InMemoryDLQStore { return m.store }

// MaxRetries returns the configured max retry count.
func (m *DLQServiceModule) MaxRetries() int { return m.config.MaxRetries }

// RetentionDays returns the configured retention period.
func (m *DLQServiceModule) RetentionDays() int { return m.config.RetentionDays }
