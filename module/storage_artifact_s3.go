package module

import (
	"context"
	"fmt"
	"io"

	"github.com/CrisisTextLine/modular"
)

// ArtifactS3Config holds configuration for the S3 artifact store module.
//
// When Endpoint is set to "local", the module falls back to a filesystem
// backend rooted at BasePath (useful for local development and testing).
//
// Full S3 implementation would use:
//   - aws-sdk-go-v2/service/s3 PutObject / GetObject / ListObjectsV2 / DeleteObject
//   - object key = Prefix + "/" + key
//   - metadata stored as S3 object user metadata (x-amz-meta-*)
//   - Exists implemented via HeadObject
type ArtifactS3Config struct {
	Bucket      string
	Prefix      string
	Region      string
	Endpoint    string // "local" → filesystem fallback; otherwise S3 endpoint URL
	BasePath    string // used when Endpoint == "local"
	Credentials struct {
		AccessKeyID     string
		SecretAccessKey string
	}
}

// ArtifactS3Module is a modular.Module that provides an S3-backed ArtifactStore.
// Module type: storage.artifact with backend: s3.
//
// For MVP: when Endpoint == "local", delegates to the filesystem backend.
// Production S3 support requires wiring the aws-sdk-go-v2 S3 client.
type ArtifactS3Module struct {
	name     string
	cfg      ArtifactS3Config
	delegate ArtifactStore
	logger   modular.Logger
}

// NewArtifactS3Module creates a new S3 artifact store module.
func NewArtifactS3Module(name string, cfg ArtifactS3Config) *ArtifactS3Module {
	return &ArtifactS3Module{
		name:   name,
		cfg:    cfg,
		logger: &noopLogger{},
	}
}

func (m *ArtifactS3Module) Name() string { return m.name }

func (m *ArtifactS3Module) Init(app modular.Application) error {
	m.logger = app.Logger()
	return nil
}

func (m *ArtifactS3Module) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{
			Name:        m.name,
			Description: "S3 artifact store",
			Instance:    m,
		},
	}
}

func (m *ArtifactS3Module) RequiresServices() []modular.ServiceDependency {
	return nil
}

func (m *ArtifactS3Module) Start(ctx context.Context) error {
	if m.cfg.Endpoint == "local" {
		basePath := m.cfg.BasePath
		if basePath == "" {
			basePath = "./data/artifacts-s3-local"
		}
		fs := NewArtifactFSModule(m.name+"-fs", ArtifactFSConfig{BasePath: basePath})
		if err := fs.Start(ctx); err != nil {
			return err
		}
		m.delegate = fs
		m.logger.Info("S3 artifact store using local filesystem fallback", "name", m.name, "path", basePath)
		return nil
	}

	// Production S3 wiring would go here:
	//   cfg, err := awsconfig.LoadDefaultConfig(ctx,
	//       awsconfig.WithRegion(m.cfg.Region),
	//       awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
	//           m.cfg.Credentials.AccessKeyID, m.cfg.Credentials.SecretAccessKey, "")),
	//   )
	//   client := s3.NewFromConfig(cfg, func(o *s3.Options) {
	//       if m.cfg.Endpoint != "" { o.BaseEndpoint = aws.String(m.cfg.Endpoint) }
	//   })
	//   m.delegate = &s3ArtifactBackend{client: client, bucket: m.cfg.Bucket, prefix: m.cfg.Prefix}
	return fmt.Errorf("artifact store %q: S3 backend requires endpoint: \"local\" for MVP; full S3 not yet wired", m.name)
}

func (m *ArtifactS3Module) Stop(_ context.Context) error {
	m.logger.Info("S3 artifact store stopped", "name", m.name)
	return nil
}

func (m *ArtifactS3Module) Upload(ctx context.Context, key string, reader io.Reader, metadata map[string]string) error {
	if m.delegate == nil {
		return fmt.Errorf("artifact store %q: not started", m.name)
	}
	return m.delegate.Upload(ctx, key, reader, metadata)
}

func (m *ArtifactS3Module) Download(ctx context.Context, key string) (io.ReadCloser, map[string]string, error) {
	if m.delegate == nil {
		return nil, nil, fmt.Errorf("artifact store %q: not started", m.name)
	}
	return m.delegate.Download(ctx, key)
}

func (m *ArtifactS3Module) List(ctx context.Context, prefix string) ([]ArtifactInfo, error) {
	if m.delegate == nil {
		return nil, fmt.Errorf("artifact store %q: not started", m.name)
	}
	return m.delegate.List(ctx, prefix)
}

func (m *ArtifactS3Module) Delete(ctx context.Context, key string) error {
	if m.delegate == nil {
		return fmt.Errorf("artifact store %q: not started", m.name)
	}
	return m.delegate.Delete(ctx, key)
}

func (m *ArtifactS3Module) Exists(ctx context.Context, key string) (bool, error) {
	if m.delegate == nil {
		return false, fmt.Errorf("artifact store %q: not started", m.name)
	}
	return m.delegate.Exists(ctx, key)
}
