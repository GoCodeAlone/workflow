package module

import (
	"context"
	"fmt"
	"io"

	"cloud.google.com/go/storage"
	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/store"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
)

// GCSStorage provides object storage operations using Google Cloud Storage.
type GCSStorage struct {
	name            string
	bucket          string
	project         string
	credentialsFile string
	client          *storage.Client
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

// Start initializes the GCS client.
func (g *GCSStorage) Start(ctx context.Context) error {
	opts := []option.ClientOption{}
	if g.credentialsFile != "" {
		opts = append(opts, option.WithAuthCredentialsFile(option.ServiceAccount, g.credentialsFile))
	}

	client, err := storage.NewClient(ctx, opts...)
	if err != nil {
		return fmt.Errorf("failed to create GCS client: %w", err)
	}

	g.client = client
	g.logger.Info("GCS storage started", "bucket", g.bucket, "project", g.project)
	return nil
}

// Stop closes the GCS client.
func (g *GCSStorage) Stop(_ context.Context) error {
	if g.client != nil {
		if err := g.client.Close(); err != nil {
			return fmt.Errorf("failed to close GCS client: %w", err)
		}
		g.client = nil
	}
	g.logger.Info("GCS storage stopped")
	return nil
}

// List returns file entries under the given prefix.
func (g *GCSStorage) List(ctx context.Context, prefix string) ([]store.FileInfo, error) {
	if g.client == nil {
		return nil, fmt.Errorf("GCS client not initialized; call Start first")
	}

	it := g.client.Bucket(g.bucket).Objects(ctx, &storage.Query{Prefix: prefix})
	var files []store.FileInfo
	for {
		attrs, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to list objects with prefix %q: %w", prefix, err)
		}
		files = append(files, store.FileInfo{
			Name:    attrs.Name,
			Path:    attrs.Name,
			Size:    attrs.Size,
			ModTime: attrs.Updated,
		})
	}
	return files, nil
}

// Get retrieves an object from GCS.
func (g *GCSStorage) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	if g.client == nil {
		return nil, fmt.Errorf("GCS client not initialized; call Start first")
	}

	r, err := g.client.Bucket(g.bucket).Object(key).NewReader(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get object %q: %w", key, err)
	}
	return r, nil
}

// Put uploads an object to GCS.
func (g *GCSStorage) Put(ctx context.Context, key string, reader io.Reader) error {
	if g.client == nil {
		return fmt.Errorf("GCS client not initialized; call Start first")
	}

	w := g.client.Bucket(g.bucket).Object(key).NewWriter(ctx)
	if _, err := io.Copy(w, reader); err != nil {
		_ = w.Close()
		return fmt.Errorf("failed to write object %q: %w", key, err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("failed to close writer for object %q: %w", key, err)
	}

	g.logger.Info("Object uploaded", "key", key, "bucket", g.bucket)
	return nil
}

// Delete removes an object from GCS.
func (g *GCSStorage) Delete(ctx context.Context, key string) error {
	if g.client == nil {
		return fmt.Errorf("GCS client not initialized; call Start first")
	}

	if err := g.client.Bucket(g.bucket).Object(key).Delete(ctx); err != nil {
		return fmt.Errorf("failed to delete object %q: %w", key, err)
	}

	g.logger.Info("Object deleted", "key", key, "bucket", g.bucket)
	return nil
}

// Stat returns metadata for an object.
func (g *GCSStorage) Stat(ctx context.Context, key string) (store.FileInfo, error) {
	if g.client == nil {
		return store.FileInfo{}, fmt.Errorf("GCS client not initialized; call Start first")
	}

	attrs, err := g.client.Bucket(g.bucket).Object(key).Attrs(ctx)
	if err != nil {
		return store.FileInfo{}, fmt.Errorf("failed to stat object %q: %w", key, err)
	}

	return store.FileInfo{
		Name:    attrs.Name,
		Path:    attrs.Name,
		Size:    attrs.Size,
		ModTime: attrs.Updated,
	}, nil
}

// MkdirAll is a no-op for object storage (GCS has no real directories).
func (g *GCSStorage) MkdirAll(_ context.Context, _ string) error {
	return nil
}
