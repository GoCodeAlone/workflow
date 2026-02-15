package module

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	"github.com/CrisisTextLine/modular"
	_ "modernc.org/sqlite"
)

// SQLiteStorage is a module that provides a SQLite database connection as a
// service. Other modules can depend on it for local SQL storage.
type SQLiteStorage struct {
	name           string
	dbPath         string
	maxConnections int
	walMode        bool
	db             *sql.DB
	logger         modular.Logger
}

// NewSQLiteStorage creates a new SQLite storage module.
func NewSQLiteStorage(name, dbPath string) *SQLiteStorage {
	return &SQLiteStorage{
		name:           name,
		dbPath:         dbPath,
		maxConnections: 5,
		walMode:        true,
		logger:         &noopLogger{},
	}
}

// SetMaxConnections sets the maximum number of database connections.
func (s *SQLiteStorage) SetMaxConnections(n int) {
	if n > 0 {
		s.maxConnections = n
	}
}

// SetWALMode enables or disables WAL journal mode.
func (s *SQLiteStorage) SetWALMode(enabled bool) {
	s.walMode = enabled
}

func (s *SQLiteStorage) Name() string { return s.name }

func (s *SQLiteStorage) Init(app modular.Application) error {
	s.logger = app.Logger()
	return nil
}

// Start opens the SQLite database connection.
func (s *SQLiteStorage) Start(_ context.Context) error {
	dir := filepath.Dir(s.dbPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create data directory %s: %w", dir, err)
	}

	dsn := s.dbPath
	if s.walMode {
		dsn += "?_journal_mode=WAL&_busy_timeout=5000"
	}

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return fmt.Errorf("open sqlite %s: %w", s.dbPath, err)
	}

	db.SetMaxOpenConns(s.maxConnections)

	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		db.Close()
		return fmt.Errorf("enable foreign keys: %w", err)
	}

	s.db = db
	s.logger.Info("SQLite storage started", "path", s.dbPath)
	return nil
}

// Stop closes the database connection.
func (s *SQLiteStorage) Stop(_ context.Context) error {
	if s.db != nil {
		s.logger.Info("SQLite storage stopped")
		return s.db.Close()
	}
	return nil
}

// DB returns the underlying *sql.DB connection.
func (s *SQLiteStorage) DB() *sql.DB {
	return s.db
}

func (s *SQLiteStorage) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{Name: s.name, Description: "SQLite database connection", Instance: s},
	}
}

func (s *SQLiteStorage) RequiresServices() []modular.ServiceDependency {
	return nil
}
