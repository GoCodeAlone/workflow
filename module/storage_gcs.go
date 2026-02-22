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

// gcsBucketHandle abstracts a GCS bucket handle for testability.
type gcsBucketHandle interface {
	Objects(ctx context.Context, q *storage.Query) objectIterator
	Object(name string) objectHandle
}

// objectIterator abstracts a GCS object iterator.
type objectIterator interface {
	Next() (*storage.ObjectAttrs, error)
}

// objectHandle abstracts a GCS object handle.
type objectHandle interface {
	NewReader(ctx context.Context) (io.ReadCloser, error)
	NewWriter(ctx context.Context) io.WriteCloser
	Delete(ctx context.Context) error
	Attrs(ctx context.Context) (*storage.ObjectAttrs, error)
}

// realBucketHandle wraps *storage.BucketHandle to satisfy gcsBucketHandle.
type realBucketHandle struct{ bh *storage.BucketHandle }

func (r *realBucketHandle) Objects(ctx context.Context, q *storage.Query) objectIterator {
	return r.bh.Objects(ctx, q)
}

func (r *realBucketHandle) Object(name string) objectHandle {
	return &realObjectHandle{r.bh.Object(name)}
}

// realObjectHandle wraps *storage.ObjectHandle to satisfy objectHandle.
type realObjectHandle struct{ oh *storage.ObjectHandle }

func (r *realObjectHandle) NewReader(ctx context.Context) (io.ReadCloser, error) {
	return r.oh.NewReader(ctx)
}

func (r *realObjectHandle) NewWriter(ctx context.Context) io.WriteCloser {
	return r.oh.NewWriter(ctx)
}

func (r *realObjectHandle) Delete(ctx context.Context) error { return r.oh.Delete(ctx) }

func (r *realObjectHandle) Attrs(ctx context.Context) (*storage.ObjectAttrs, error) {
	return r.oh.Attrs(ctx)
}

// GCSStorage provides object storage operations using Google Cloud Storage.
type GCSStorage struct {
	name            string
	bucket          string
	project         string
	credentialsFile string
	client          *storage.Client
	testBucket      gcsBucketHandle // non-nil only in tests
	logger          modular.Logger
}

// setBucketHandle injects a gcsBucketHandle, used in tests to avoid real GCS calls.
func (g *GCSStorage) setBucketHandle(bh gcsBucketHandle) { g.testBucket = bh }

// getBucket returns the bucket handle, preferring the injected test handle.
func (g *GCSStorage) getBucket() gcsBucketHandle {
	if g.testBucket != nil {
		return g.testBucket
	}
	return &realBucketHandle{g.client.Bucket(g.bucket)}
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
	if g.project != "" {
		opts = append(opts, option.WithQuotaProject(g.project))
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
	if g.client == nil && g.testBucket == nil {
		return nil, fmt.Errorf("GCS client not initialized; call Start first")
	}

	it := g.getBucket().Objects(ctx, &storage.Query{Prefix: prefix})
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
	if g.client == nil && g.testBucket == nil {
		return nil, fmt.Errorf("GCS client not initialized; call Start first")
	}

	r, err := g.getBucket().Object(key).NewReader(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get object %q: %w", key, err)
	}
	return r, nil
}

// Put uploads an object to GCS.
func (g *GCSStorage) Put(ctx context.Context, key string, reader io.Reader) error {
	if g.client == nil && g.testBucket == nil {
		return fmt.Errorf("GCS client not initialized; call Start first")
	}

	w := g.getBucket().Object(key).NewWriter(ctx)
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
	if g.client == nil && g.testBucket == nil {
		return fmt.Errorf("GCS client not initialized; call Start first")
	}

	if err := g.getBucket().Object(key).Delete(ctx); err != nil {
		return fmt.Errorf("failed to delete object %q: %w", key, err)
	}

	g.logger.Info("Object deleted", "key", key, "bucket", g.bucket)
	return nil
}

// Stat returns metadata for an object.
func (g *GCSStorage) Stat(ctx context.Context, key string) (store.FileInfo, error) {
	if g.client == nil && g.testBucket == nil {
		return store.FileInfo{}, fmt.Errorf("GCS client not initialized; call Start first")
	}

	attrs, err := g.getBucket().Object(key).Attrs(ctx)
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
