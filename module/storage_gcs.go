package module

import (
	"context"
	"fmt"
	"io"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/store"
)

// GCSStorage provides object storage operations using Google Cloud Storage.
// This is a stub implementation that follows the same pattern as S3Storage.
type GCSStorage struct {
	name            string
	bucket          string
	project         string
	credentialsFile string
	logger          modular.Logger
}

// NewGCSStorage creates a new GCS storage module.
func NewGCSStorage(name string) *GCSStorage {
	return &GCSStorage{
		name:   name,
		logger: &noopLogger{},
	}
}

func (g *GCSStorage) Name() string { return g.name }

func (g *GCSStorage) Init(app modular.Application) error {
	g.logger = app.Logger()
	return nil
}

func (g *GCSStorage) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{
			Name:        g.name,
			Description: "Google Cloud Storage",
			Instance:    g,
		},
	}
}

func (g *GCSStorage) RequiresServices() []modular.ServiceDependency {
	return nil
}

// SetBucket sets the GCS bucket name.
func (g *GCSStorage) SetBucket(bucket string) { g.bucket = bucket }

// SetProject sets the GCP project ID.
func (g *GCSStorage) SetProject(project string) { g.project = project }

// SetCredentialsFile sets the path to a service account JSON key file.
func (g *GCSStorage) SetCredentialsFile(path string) { g.credentialsFile = path }

func (g *GCSStorage) Start(_ context.Context) error {
	g.logger.Info("GCS storage started (stub)", "bucket", g.bucket, "project", g.project)
	return nil
}

func (g *GCSStorage) Stop(_ context.Context) error {
	g.logger.Info("GCS storage stopped")
	return nil
}

// --- StorageProvider interface (stub) ---

func (g *GCSStorage) List(_ context.Context, _ string) ([]store.FileInfo, error) {
	return nil, fmt.Errorf("GCS storage not yet implemented")
}

func (g *GCSStorage) Get(_ context.Context, _ string) (io.ReadCloser, error) {
	return nil, fmt.Errorf("GCS storage not yet implemented")
}

func (g *GCSStorage) Put(_ context.Context, _ string, _ io.Reader) error {
	return fmt.Errorf("GCS storage not yet implemented")
}

func (g *GCSStorage) Delete(_ context.Context, _ string) error {
	return fmt.Errorf("GCS storage not yet implemented")
}

func (g *GCSStorage) Stat(_ context.Context, _ string) (store.FileInfo, error) {
	return store.FileInfo{}, fmt.Errorf("GCS storage not yet implemented")
}
