package module

import (
	"context"
	"io"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/store"
)

// LocalStorageModule wraps a store.LocalStorage as a modular.Module.
type LocalStorageModule struct {
	name    string
	rootDir string
	storage *store.LocalStorage
	logger  modular.Logger
}

// NewLocalStorageModule creates a new local filesystem storage module.
func NewLocalStorageModule(name, rootDir string) *LocalStorageModule {
	return &LocalStorageModule{
		name:    name,
		rootDir: rootDir,
		logger:  &noopLogger{},
	}
}

func (m *LocalStorageModule) Name() string { return m.name }

func (m *LocalStorageModule) Init(app modular.Application) error {
	m.logger = app.Logger()
	return nil
}

func (m *LocalStorageModule) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{
			Name:        m.name,
			Description: "Local filesystem storage",
			Instance:    m,
		},
	}
}

func (m *LocalStorageModule) RequiresServices() []modular.ServiceDependency {
	return nil
}

func (m *LocalStorageModule) Start(_ context.Context) error {
	ls, err := store.NewLocalStorage(m.rootDir)
	if err != nil {
		return err
	}
	m.storage = ls
	m.logger.Info("Local storage started", "root", m.rootDir)
	return nil
}

func (m *LocalStorageModule) Stop(_ context.Context) error {
	m.logger.Info("Local storage stopped")
	return nil
}

// Storage returns the underlying StorageProvider, or nil if not started.
func (m *LocalStorageModule) Storage() store.StorageProvider {
	return m.storage
}

// --- Implement store.StorageProvider directly so the module itself can be used ---

func (m *LocalStorageModule) List(ctx context.Context, prefix string) ([]store.FileInfo, error) {
	return m.storage.List(ctx, prefix)
}

func (m *LocalStorageModule) Get(ctx context.Context, path string) (io.ReadCloser, error) {
	return m.storage.Get(ctx, path)
}

func (m *LocalStorageModule) Put(ctx context.Context, path string, reader io.Reader) error {
	return m.storage.Put(ctx, path, reader)
}

func (m *LocalStorageModule) Delete(ctx context.Context, path string) error {
	return m.storage.Delete(ctx, path)
}

func (m *LocalStorageModule) Stat(ctx context.Context, path string) (store.FileInfo, error) {
	return m.storage.Stat(ctx, path)
}
