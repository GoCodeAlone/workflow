package module

import (
	"context"
	"fmt"
	"io"

	"github.com/CrisisTextLine/modular"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// S3Storage provides object storage operations using AWS S3.
// It implements the modular.Module interface.
type S3Storage struct {
	name     string
	bucket   string
	region   string
	endpoint string
	client   *s3.Client
	logger   modular.Logger
}

// NewS3Storage creates a new S3 storage module.
func NewS3Storage(name string) *S3Storage {
	return &S3Storage{
		name:   name,
		region: "us-east-1",
		logger: &noopLogger{},
	}
}

// Name returns the module name.
func (s *S3Storage) Name() string {
	return s.name
}

// Init initializes the module with the application context.
func (s *S3Storage) Init(app modular.Application) error {
	s.logger = app.Logger()
	return nil
}

// ProvidesServices returns the services provided by this module.
func (s *S3Storage) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{
			Name:        s.name,
			Description: "S3 Storage",
			Instance:    s,
		},
	}
}

// RequiresServices returns the services required by this module.
func (s *S3Storage) RequiresServices() []modular.ServiceDependency {
	return nil
}

// SetBucket sets the S3 bucket name.
func (s *S3Storage) SetBucket(bucket string) {
	s.bucket = bucket
}

// SetRegion sets the AWS region.
func (s *S3Storage) SetRegion(region string) {
	s.region = region
}

// SetEndpoint sets a custom endpoint (for LocalStack/MinIO).
func (s *S3Storage) SetEndpoint(endpoint string) {
	s.endpoint = endpoint
}

// SetClient sets a custom S3 client (useful for testing).
func (s *S3Storage) SetClient(client *s3.Client) {
	s.client = client
}

// Start initializes the S3 client.
func (s *S3Storage) Start(ctx context.Context) error {
	opts := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(s.region),
	}

	cfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return fmt.Errorf("failed to load AWS config: %w", err)
	}

	s3Opts := []func(*s3.Options){}
	if s.endpoint != "" {
		s3Opts = append(s3Opts, func(o *s3.Options) {
			o.BaseEndpoint = &s.endpoint
			o.UsePathStyle = true
		})
	}

	s.client = s3.NewFromConfig(cfg, s3Opts...)
	s.logger.Info("S3 storage started", "bucket", s.bucket, "region", s.region)
	return nil
}

// Stop is a no-op for S3 storage.
func (s *S3Storage) Stop(_ context.Context) error {
	s.logger.Info("S3 storage stopped")
	return nil
}

// PutObject uploads an object to S3.
func (s *S3Storage) PutObject(ctx context.Context, key string, body io.Reader) error {
	if s.client == nil {
		return fmt.Errorf("S3 client not initialized; call Start first")
	}

	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: &s.bucket,
		Key:    &key,
		Body:   body,
	})
	if err != nil {
		return fmt.Errorf("failed to put object %q: %w", key, err)
	}

	s.logger.Info("Object uploaded", "key", key, "bucket", s.bucket)
	return nil
}

// GetObject retrieves an object from S3.
func (s *S3Storage) GetObject(ctx context.Context, key string) (io.ReadCloser, error) {
	if s.client == nil {
		return nil, fmt.Errorf("S3 client not initialized; call Start first")
	}

	result, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: &s.bucket,
		Key:    &key,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get object %q: %w", key, err)
	}

	return result.Body, nil
}

// DeleteObject removes an object from S3.
func (s *S3Storage) DeleteObject(ctx context.Context, key string) error {
	if s.client == nil {
		return fmt.Errorf("S3 client not initialized; call Start first")
	}

	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: &s.bucket,
		Key:    &key,
	})
	if err != nil {
		return fmt.Errorf("failed to delete object %q: %w", key, err)
	}

	s.logger.Info("Object deleted", "key", key, "bucket", s.bucket)
	return nil
}
