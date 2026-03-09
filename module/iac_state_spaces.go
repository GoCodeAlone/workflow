package module

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// SpacesS3Client abstracts the S3 API methods used by SpacesIaCStateStore,
// allowing a mock to be injected for testing.
type SpacesS3Client interface {
	GetObject(ctx context.Context, input *s3.GetObjectInput, opts ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	PutObject(ctx context.Context, input *s3.PutObjectInput, opts ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	DeleteObject(ctx context.Context, input *s3.DeleteObjectInput, opts ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
	ListObjectsV2(ctx context.Context, input *s3.ListObjectsV2Input, opts ...func(*s3.Options)) (*s3.ListObjectsV2Output, error)
	HeadObject(ctx context.Context, input *s3.HeadObjectInput, opts ...func(*s3.Options)) (*s3.HeadObjectOutput, error)
}

// SpacesIaCStateStore persists IaC state as JSON objects in a DigitalOcean Spaces
// bucket (or any S3-compatible store). Lock objects are used for advisory locking.
type SpacesIaCStateStore struct {
	client SpacesS3Client
	bucket string
	prefix string
	mu     sync.Mutex
}

// NewSpacesIaCStateStore creates a Spaces/S3-compatible state store.
//
// Parameters:
//   - region: DO region (e.g. "nyc3"); used to construct the endpoint
//     https://<region>.digitaloceanspaces.com unless endpoint is set.
//   - bucket: Spaces bucket name (required).
//   - prefix: optional key prefix (default "iac-state/").
//   - accessKey: Spaces access key; falls back to DO_SPACES_ACCESS_KEY env var.
//   - secretKey: Spaces secret key; falls back to DO_SPACES_SECRET_KEY env var.
//   - endpoint: optional custom endpoint override.
func NewSpacesIaCStateStore(region, bucket, prefix, accessKey, secretKey, endpoint string) (*SpacesIaCStateStore, error) {
	if bucket == "" {
		return nil, fmt.Errorf("iac spaces state: bucket must not be empty")
	}
	if prefix == "" {
		prefix = "iac-state/"
	}
	if accessKey == "" {
		accessKey = os.Getenv("DO_SPACES_ACCESS_KEY")
	}
	if secretKey == "" {
		secretKey = os.Getenv("DO_SPACES_SECRET_KEY")
	}
	if endpoint == "" && region != "" {
		endpoint = fmt.Sprintf("https://%s.digitaloceanspaces.com", region)
	}
	if endpoint == "" {
		return nil, fmt.Errorf("iac spaces state: either region or endpoint must be set")
	}

	cfg, err := awsconfig.LoadDefaultConfig(context.Background(),
		awsconfig.WithRegion(regionOrDefault(region)),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKey, secretKey, "")),
	)
	if err != nil {
		return nil, fmt.Errorf("iac spaces state: load config: %w", err)
	}

	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = &endpoint
		o.UsePathStyle = true
	})

	return &SpacesIaCStateStore{
		client: client,
		bucket: bucket,
		prefix: prefix,
	}, nil
}

// NewSpacesIaCStateStoreWithClient creates a store with an injected client (for testing).
func NewSpacesIaCStateStoreWithClient(client SpacesS3Client, bucket, prefix string) *SpacesIaCStateStore {
	if prefix == "" {
		prefix = "iac-state/"
	}
	return &SpacesIaCStateStore{
		client: client,
		bucket: bucket,
		prefix: prefix,
	}
}

func regionOrDefault(region string) string {
	if region == "" {
		return "us-east-1"
	}
	return region
}

// stateKey returns the S3 key for a resource's state JSON.
func (s *SpacesIaCStateStore) stateKey(resourceID string) string {
	return s.prefix + sanitizeID(resourceID) + ".json"
}

// lockKey returns the S3 key for a resource's lock object.
func (s *SpacesIaCStateStore) lockKey(resourceID string) string {
	return s.prefix + sanitizeID(resourceID) + ".lock"
}

// GetState retrieves a state record by resource ID. Returns nil, nil when not found.
func (s *SpacesIaCStateStore) GetState(resourceID string) (*IaCState, error) {
	key := s.stateKey(resourceID)
	out, err := s.client.GetObject(context.Background(), &s3.GetObjectInput{
		Bucket: &s.bucket,
		Key:    &key,
	})
	if err != nil {
		if isNotFoundErr(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("iac spaces state: GetState %q: %w", resourceID, err)
	}
	defer out.Body.Close()

	data, err := io.ReadAll(out.Body)
	if err != nil {
		return nil, fmt.Errorf("iac spaces state: GetState %q: read body: %w", resourceID, err)
	}

	var st IaCState
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("iac spaces state: GetState %q: unmarshal: %w", resourceID, err)
	}
	return &st, nil
}

// SaveState writes the state record as a JSON object to Spaces.
func (s *SpacesIaCStateStore) SaveState(state *IaCState) error {
	if state == nil {
		return fmt.Errorf("iac spaces state: SaveState: state must not be nil")
	}
	if state.ResourceID == "" {
		return fmt.Errorf("iac spaces state: SaveState: resource_id must not be empty")
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("iac spaces state: SaveState %q: marshal: %w", state.ResourceID, err)
	}

	key := s.stateKey(state.ResourceID)
	contentType := "application/json"
	_, err = s.client.PutObject(context.Background(), &s3.PutObjectInput{
		Bucket:      &s.bucket,
		Key:         &key,
		Body:        bytes.NewReader(data),
		ContentType: &contentType,
	})
	if err != nil {
		return fmt.Errorf("iac spaces state: SaveState %q: put: %w", state.ResourceID, err)
	}
	return nil
}

// ListStates lists all state objects under the prefix and returns those matching filter.
// Supported filter keys: "resource_type", "provider", "status".
func (s *SpacesIaCStateStore) ListStates(filter map[string]string) ([]*IaCState, error) {
	var results []*IaCState
	var continuationToken *string

	for {
		out, err := s.client.ListObjectsV2(context.Background(), &s3.ListObjectsV2Input{
			Bucket:            &s.bucket,
			Prefix:            &s.prefix,
			ContinuationToken: continuationToken,
		})
		if err != nil {
			return nil, fmt.Errorf("iac spaces state: ListStates: %w", err)
		}

		for _, obj := range out.Contents {
			key := aws.ToString(obj.Key)
			// Skip lock files and non-JSON objects.
			if strings.HasSuffix(key, ".lock") || !strings.HasSuffix(key, ".json") {
				continue
			}

			getOut, err := s.client.GetObject(context.Background(), &s3.GetObjectInput{
				Bucket: &s.bucket,
				Key:    obj.Key,
			})
			if err != nil {
				continue // skip unreadable objects
			}
			data, err := io.ReadAll(getOut.Body)
			getOut.Body.Close()
			if err != nil {
				continue
			}

			var st IaCState
			if err := json.Unmarshal(data, &st); err != nil {
				continue
			}
			if matchesFilter(&st, filter) {
				results = append(results, &st)
			}
		}

		if !aws.ToBool(out.IsTruncated) {
			break
		}
		continuationToken = out.NextContinuationToken
	}

	return results, nil
}

// DeleteState removes the state object for resourceID.
func (s *SpacesIaCStateStore) DeleteState(resourceID string) error {
	// Verify existence first to return a meaningful error.
	key := s.stateKey(resourceID)
	_, err := s.client.HeadObject(context.Background(), &s3.HeadObjectInput{
		Bucket: &s.bucket,
		Key:    &key,
	})
	if err != nil {
		if isNotFoundErr(err) {
			return fmt.Errorf("iac spaces state: DeleteState %q: not found", resourceID)
		}
		return fmt.Errorf("iac spaces state: DeleteState %q: head: %w", resourceID, err)
	}

	_, err = s.client.DeleteObject(context.Background(), &s3.DeleteObjectInput{
		Bucket: &s.bucket,
		Key:    &key,
	})
	if err != nil {
		return fmt.Errorf("iac spaces state: DeleteState %q: %w", resourceID, err)
	}
	return nil
}

// Lock creates a lock object for resourceID. Fails if the lock already exists.
func (s *SpacesIaCStateStore) Lock(resourceID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := s.lockKey(resourceID)

	// Check if lock exists.
	_, err := s.client.HeadObject(context.Background(), &s3.HeadObjectInput{
		Bucket: &s.bucket,
		Key:    &key,
	})
	if err == nil {
		return fmt.Errorf("iac spaces state: Lock %q: resource is already locked", resourceID)
	}
	if !isNotFoundErr(err) {
		return fmt.Errorf("iac spaces state: Lock %q: head: %w", resourceID, err)
	}

	// Create lock object with a timestamp.
	body := []byte(time.Now().UTC().Format(time.RFC3339))
	_, err = s.client.PutObject(context.Background(), &s3.PutObjectInput{
		Bucket: &s.bucket,
		Key:    &key,
		Body:   bytes.NewReader(body),
	})
	if err != nil {
		return fmt.Errorf("iac spaces state: Lock %q: put: %w", resourceID, err)
	}
	return nil
}

// Unlock removes the lock object for resourceID.
func (s *SpacesIaCStateStore) Unlock(resourceID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := s.lockKey(resourceID)

	// Verify lock exists.
	_, err := s.client.HeadObject(context.Background(), &s3.HeadObjectInput{
		Bucket: &s.bucket,
		Key:    &key,
	})
	if err != nil {
		if isNotFoundErr(err) {
			return fmt.Errorf("iac spaces state: Unlock %q: not locked", resourceID)
		}
		return fmt.Errorf("iac spaces state: Unlock %q: head: %w", resourceID, err)
	}

	_, err = s.client.DeleteObject(context.Background(), &s3.DeleteObjectInput{
		Bucket: &s.bucket,
		Key:    &key,
	})
	if err != nil {
		return fmt.Errorf("iac spaces state: Unlock %q: %w", resourceID, err)
	}
	return nil
}

// isNotFoundErr checks whether an S3 error indicates the key was not found.
func isNotFoundErr(err error) bool {
	var nsk *types.NoSuchKey
	if errors.As(err, &nsk) {
		return true
	}
	// HeadObject returns a generic "NotFound" status, not NoSuchKey.
	var nf *types.NotFound
	if errors.As(err, &nf) {
		return true
	}
	// Some S3-compatible stores return a plain "not found" in the message.
	return strings.Contains(err.Error(), "NotFound") || strings.Contains(err.Error(), "NoSuchKey")
}
