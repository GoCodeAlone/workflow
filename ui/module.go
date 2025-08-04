package ui

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	_ "modernc.org/sqlite" // SQLite driver

	"github.com/GoCodeAlone/modular"
)

// UIModule provides web UI functionality for the workflow engine
type UIModule struct {
	name         string
	config       UIConfig
	server       *http.Server
	dbService    *DatabaseService
	authService  *AuthService
	apiHandler   *APIHandler
	logger       modular.Logger
}

// UIConfig represents the configuration for the UI module
type UIConfig struct {
	Address    string `yaml:"address" json:"address"`       // Server address (e.g., ":8080")
	StaticDir  string `yaml:"staticDir" json:"staticDir"`   // Directory for static files
	SecretKey  string `yaml:"secretKey" json:"secretKey"`   // JWT secret key
	DatabaseDB string `yaml:"database" json:"database"`     // Database connection string
}

// NewUIModule creates a new UI module
func NewUIModule(name string, config map[string]interface{}) *UIModule {
	uiConfig := UIConfig{
		Address:   ":8080",
		StaticDir: "./ui/static",
		SecretKey: "default-secret-key", // Should be overridden in production
	}

	// Parse configuration
	if addr, ok := config["address"].(string); ok {
		uiConfig.Address = addr
	}
	if staticDir, ok := config["staticDir"].(string); ok {
		uiConfig.StaticDir = staticDir
	}
	if secretKey, ok := config["secretKey"].(string); ok {
		uiConfig.SecretKey = secretKey
	}
	if dbPath, ok := config["database"].(string); ok {
		uiConfig.DatabaseDB = dbPath
	}

	return &UIModule{
		name:   name,
		config: uiConfig,
	}
}

// Name returns the module name
func (m *UIModule) Name() string {
	return m.name
}

// Dependencies returns the module dependencies
func (m *UIModule) Dependencies() []string {
	return []string{"database"}
}

// Configure sets up the UI module
func (m *UIModule) Configure(app modular.Application) error {
	return nil // Configuration is handled in Init
}

// Init initializes the UI module
func (m *UIModule) Init(app modular.Application) error {
	m.logger = app.Logger()

	// Get database connection - try to get existing database service first
	var db *sql.DB
	if dbService := app.SvcRegistry()["database"]; dbService != nil {
		if sqlDB, ok := dbService.(*sql.DB); ok {
			db = sqlDB
		}
	}
	
	if db == nil {
		// If no database service is available, create SQLite database
		m.logger.Info("No database service found, creating SQLite database")
		var err error
		db, err = sql.Open("sqlite", "./workflow_ui.db")
		if err != nil {
			return fmt.Errorf("failed to open database: %w", err)
		}
	}
	
	m.dbService = NewDatabaseService(db, m.logger)

	// Initialize database schema
	if err := m.dbService.InitializeSchema(context.Background()); err != nil {
		return fmt.Errorf("failed to initialize database schema: %w", err)
	}

	// Create authentication service
	m.authService = NewAuthService(m.config.SecretKey, m.dbService)

	// Create API handler
	m.apiHandler = NewAPIHandler(m.dbService, m.authService, m.logger)

	// Setup HTTP server
	r := chi.NewRouter()

	// Middleware
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RealIP)
	r.Use(middleware.Heartbeat("/ping"))

	// CORS middleware for frontend development
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Accept, Authorization, Content-Type, X-CSRF-Token")

			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusOK)
				return
			}

			next.ServeHTTP(w, r)
		})
	})

	// Setup API routes
	m.apiHandler.SetupRoutes(r)

	// Serve static files
	staticHandler := http.StripPrefix("/", http.FileServer(http.Dir(m.config.StaticDir)))
	r.Handle("/*", staticHandler)

	m.server = &http.Server{
		Addr:    m.config.Address,
		Handler: r,
	}

	// Register services
	if err := app.RegisterService("ui-database", m.dbService); err != nil {
		return fmt.Errorf("failed to register database service: %w", err)
	}
	if err := app.RegisterService("ui-auth", m.authService); err != nil {
		return fmt.Errorf("failed to register auth service: %w", err)
	}
	if err := app.RegisterService("ui-api", m.apiHandler); err != nil {
		return fmt.Errorf("failed to register api service: %w", err)
	}

	m.logger.Info("UI module configured", "address", m.config.Address, "staticDir", m.config.StaticDir)

	return nil
}

// Start starts the UI module
func (m *UIModule) Start(ctx context.Context) error {
	m.logger.Info("Starting UI server", "address", m.config.Address)

	go func() {
		if err := m.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			m.logger.Error("UI server error", "error", err)
		}
	}()

	return nil
}

// Stop stops the UI module
func (m *UIModule) Stop(ctx context.Context) error {
	m.logger.Info("Stopping UI server")
	return m.server.Shutdown(ctx)
}