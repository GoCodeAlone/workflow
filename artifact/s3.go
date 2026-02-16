package artifact

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"path"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// S3Store implements Store using an S3-compatible backend.
// Objects are stored under {prefix}/artifacts/{executionID}/{key}.
type S3Store struct {
	client *s3.Client
	bucket string
	prefix string
}

// NewS3Store creates a new S3Store.
func NewS3Store(client *s3.Client, bucket, prefix string) *S3Store {
	return &S3Store{
		client: client,
		bucket: bucket,
		prefix: prefix,
	}
}

// objectKey returns the full S3 key for a given artifact.
func (s *S3Store) objectKey(executionID, key string) string {
	return path.Join(s.prefix, "artifacts", executionID, key)
}

// Put uploads an artifact to S3. The reader content is buffered to compute
// the SHA256 checksum before upload, since S3 PutObject requires a seekable body
// or known content length for checksum metadata.
func (s *S3Store) Put(ctx context.Context, executionID, key string, reader io.Reader) error {
	// Read all content to compute checksum and size.
	data, err := io.ReadAll(reader)
	if err != nil {
		return fmt.Errorf("failed to read artifact data: %w", err)
	}

	hasher := sha256.New()
	hasher.Write(data)
	checksum := hex.EncodeToString(hasher.Sum(nil))
	size := int64(len(data))

	objectKey := s.objectKey(executionID, key)

	// Store checksum and metadata as S3 object metadata.
	metadata := map[string]string{
		"checksum":   checksum,
		"size":       fmt.Sprintf("%d", size),
		"created-at": time.Now().UTC().Format(time.RFC3339),
	}

	body := newReadSeekCloser(data)

	_, err = s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:   &s.bucket,
		Key:      &objectKey,
		Body:     body,
		Metadata: metadata,
	})
	if err != nil {
		return fmt.Errorf("failed to put artifact to S3: %w", err)
	}

	return nil
}

// Get retrieves an artifact from S3.
func (s *S3Store) Get(ctx context.Context, executionID, key string) (io.ReadCloser, error) {
	objectKey := s.objectKey(executionID, key)

	result, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: &s.bucket,
		Key:    &objectKey,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get artifact from S3: %w", err)
	}

	return result.Body, nil
}

// List returns all artifacts for a given execution ID by listing S3 objects
// under the execution prefix.
func (s *S3Store) List(ctx context.Context, executionID string) ([]Artifact, error) {
	prefix := path.Join(s.prefix, "artifacts", executionID) + "/"

	result, err := s.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket: &s.bucket,
		Prefix: &prefix,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list artifacts from S3: %w", err)
	}

	var artifacts []Artifact
	for _, obj := range result.Contents {
		key := path.Base(*obj.Key)

		// Retrieve object metadata for checksum.
		head, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
			Bucket: &s.bucket,
			Key:    obj.Key,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to head artifact %q: %w", key, err)
		}

		checksum := ""
		if head.Metadata != nil {
			checksum = head.Metadata["checksum"]
		}

		var createdAt time.Time
		if head.Metadata != nil {
			if ts, ok := head.Metadata["created-at"]; ok {
				createdAt, _ = time.Parse(time.RFC3339, ts)
			}
		}
		if createdAt.IsZero() && obj.LastModified != nil {
			createdAt = *obj.LastModified
		}

		var size int64
		if obj.Size != nil {
			size = *obj.Size
		}

		artifacts = append(artifacts, Artifact{
			Key:       key,
			Size:      size,
			CreatedAt: createdAt,
			Checksum:  checksum,
		})
	}

	return artifacts, nil
}

// Delete removes an artifact from S3.
func (s *S3Store) Delete(ctx context.Context, executionID, key string) error {
	objectKey := s.objectKey(executionID, key)

	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: &s.bucket,
		Key:    &objectKey,
	})
	if err != nil {
		return fmt.Errorf("failed to delete artifact from S3: %w", err)
	}

	return nil
}

// readSeekCloser wraps a byte slice to satisfy io.ReadSeekCloser.
type readSeekCloser struct {
	data   []byte
	offset int
}

func newReadSeekCloser(data []byte) *readSeekCloser {
	return &readSeekCloser{data: data}
}

func (r *readSeekCloser) Read(p []byte) (int, error) {
	if r.offset >= len(r.data) {
		return 0, io.EOF
	}
	n := copy(p, r.data[r.offset:])
	r.offset += n
	return n, nil
}

func (r *readSeekCloser) Seek(offset int64, whence int) (int64, error) {
	var abs int64
	switch whence {
	case io.SeekStart:
		abs = offset
	case io.SeekCurrent:
		abs = int64(r.offset) + offset
	case io.SeekEnd:
		abs = int64(len(r.data)) + offset
	default:
		return 0, fmt.Errorf("invalid whence: %d", whence)
	}
	if abs < 0 {
		return 0, fmt.Errorf("negative seek position: %d", abs)
	}
	r.offset = int(abs)
	return abs, nil
}

func (r *readSeekCloser) Close() error {
	return nil
}
