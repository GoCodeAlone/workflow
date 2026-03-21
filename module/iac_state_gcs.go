package module

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"cloud.google.com/go/storage"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
)

// ErrGCSNotFound is returned by GCSObjectClient when an object does not exist.
var ErrGCSNotFound = errors.New("gcs: object not found")

// GCSObjectClient abstracts the GCS operations used by GCSIaCStateStore,
// allowing a mock to be injected for testing.
type GCSObjectClient interface {
	ReadObject(ctx context.Context, key string) (data []byte, generation int64, err error)
	WriteObject(ctx context.Context, key string, data []byte, contentType string) (generation int64, err error)
	WriteObjectIfGenerationMatch(ctx context.Context, key string, data []byte, contentType string, generation int64) (newGeneration int64, err error)
	DeleteObject(ctx context.Context, key string) error
	ListObjects(ctx context.Context, prefix string) ([]string, error)
}

// GCSIaCStateStore persists IaC state as JSON objects in Google Cloud Storage.
// Locking uses GCS generation-match preconditions for atomic, race-free lock acquisition.
type GCSIaCStateStore struct {
	client    GCSObjectClient
	bucket    string
	prefix    string
	lockState map[string]int64 // resource -> lock object generation (in-memory tracking)
}

// NewGCSIaCStateStore creates a GCS-backed state store using Application Default Credentials.
func NewGCSIaCStateStore(ctx context.Context, bucket, prefix string, opts ...option.ClientOption) (*GCSIaCStateStore, error) {
	if bucket == "" {
		return nil, fmt.Errorf("iac gcs state: bucket must not be empty")
	}
	if prefix == "" {
		prefix = "iac-state/"
	}
	gcsClient, err := storage.NewClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("iac gcs state: create client: %w", err)
	}
	return &GCSIaCStateStore{
		client:    &gcsRealClient{client: gcsClient, bucket: bucket},
		bucket:    bucket,
		prefix:    prefix,
		lockState: make(map[string]int64),
	}, nil
}

// NewGCSIaCStateStoreWithClient creates a GCS state store with an injected client (for testing).
func NewGCSIaCStateStoreWithClient(client GCSObjectClient, bucket, prefix string) *GCSIaCStateStore {
	if prefix == "" {
		prefix = "iac-state/"
	}
	return &GCSIaCStateStore{
		client:    client,
		bucket:    bucket,
		prefix:    prefix,
		lockState: make(map[string]int64),
	}
}

func (s *GCSIaCStateStore) stateKey(resourceID string) string {
	return s.prefix + sanitizeID(resourceID) + ".json"
}

func (s *GCSIaCStateStore) lockKey(resourceID string) string {
	return s.prefix + sanitizeID(resourceID) + ".lock"
}

// GetState retrieves a state record by resource ID. Returns nil, nil when not found.
func (s *GCSIaCStateStore) GetState(resourceID string) (*IaCState, error) {
	data, _, err := s.client.ReadObject(context.Background(), s.stateKey(resourceID))
	if err != nil {
		if errors.Is(err, ErrGCSNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("iac gcs state: GetState %q: %w", resourceID, err)
	}
	var st IaCState
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("iac gcs state: GetState %q: unmarshal: %w", resourceID, err)
	}
	return &st, nil
}

// SaveState writes the state record as a JSON object to GCS.
func (s *GCSIaCStateStore) SaveState(state *IaCState) error {
	if state == nil {
		return fmt.Errorf("iac gcs state: SaveState: state must not be nil")
	}
	if state.ResourceID == "" {
		return fmt.Errorf("iac gcs state: SaveState: resource_id must not be empty")
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("iac gcs state: SaveState %q: marshal: %w", state.ResourceID, err)
	}
	if _, err := s.client.WriteObject(context.Background(), s.stateKey(state.ResourceID), data, "application/json"); err != nil {
		return fmt.Errorf("iac gcs state: SaveState %q: write: %w", state.ResourceID, err)
	}
	return nil
}

// ListStates lists all state objects and returns those matching the filter.
func (s *GCSIaCStateStore) ListStates(filter map[string]string) ([]*IaCState, error) {
	keys, err := s.client.ListObjects(context.Background(), s.prefix)
	if err != nil {
		return nil, fmt.Errorf("iac gcs state: ListStates: %w", err)
	}
	var results []*IaCState
	for _, key := range keys {
		if !strings.HasSuffix(key, ".json") {
			continue
		}
		data, _, err := s.client.ReadObject(context.Background(), key)
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
	return results, nil
}

// DeleteState removes the state object for resourceID.
func (s *GCSIaCStateStore) DeleteState(resourceID string) error {
	if err := s.client.DeleteObject(context.Background(), s.stateKey(resourceID)); err != nil {
		if errors.Is(err, ErrGCSNotFound) {
			return fmt.Errorf("iac gcs state: DeleteState %q: not found", resourceID)
		}
		return fmt.Errorf("iac gcs state: DeleteState %q: %w", resourceID, err)
	}
	return nil
}

// Lock acquires an advisory lock using GCS generation-match preconditions.
// The lock object is written with If-None-Match (generation 0), which is atomic.
func (s *GCSIaCStateStore) Lock(resourceID string) error {
	key := s.lockKey(resourceID)
	body := []byte("locked")
	gen, err := s.client.WriteObjectIfGenerationMatch(context.Background(), key, body, "text/plain", 0)
	if err != nil {
		if strings.Contains(err.Error(), "precondition failed") || strings.Contains(err.Error(), "exists") {
			return fmt.Errorf("iac gcs state: Lock %q: resource is already locked", resourceID)
		}
		return fmt.Errorf("iac gcs state: Lock %q: %w", resourceID, err)
	}
	s.lockState[resourceID] = gen
	return nil
}

// Unlock removes the lock object for resourceID.
func (s *GCSIaCStateStore) Unlock(resourceID string) error {
	key := s.lockKey(resourceID)
	if err := s.client.DeleteObject(context.Background(), key); err != nil {
		if errors.Is(err, ErrGCSNotFound) {
			return fmt.Errorf("iac gcs state: Unlock %q: not locked", resourceID)
		}
		return fmt.Errorf("iac gcs state: Unlock %q: %w", resourceID, err)
	}
	delete(s.lockState, resourceID)
	return nil
}

// gcsRealClient wraps the real GCS client to satisfy GCSObjectClient.
type gcsRealClient struct {
	client *storage.Client
	bucket string
}

func (c *gcsRealClient) ReadObject(ctx context.Context, key string) ([]byte, int64, error) {
	obj := c.client.Bucket(c.bucket).Object(key)
	attrs, err := obj.Attrs(ctx)
	if err != nil {
		if errors.Is(err, storage.ErrObjectNotExist) {
			return nil, 0, ErrGCSNotFound
		}
		return nil, 0, err
	}
	r, err := obj.NewReader(ctx)
	if err != nil {
		return nil, 0, err
	}
	defer r.Close()
	buf := new(bytes.Buffer)
	if _, err := buf.ReadFrom(r); err != nil {
		return nil, 0, err
	}
	return buf.Bytes(), attrs.Generation, nil
}

func (c *gcsRealClient) WriteObject(ctx context.Context, key string, data []byte, contentType string) (int64, error) {
	w := c.client.Bucket(c.bucket).Object(key).NewWriter(ctx)
	w.ContentType = contentType
	if _, err := w.Write(data); err != nil {
		_ = w.Close()
		return 0, err
	}
	if err := w.Close(); err != nil {
		return 0, err
	}
	attrs, err := c.client.Bucket(c.bucket).Object(key).Attrs(ctx)
	if err != nil {
		return 0, err
	}
	return attrs.Generation, nil
}

func (c *gcsRealClient) WriteObjectIfGenerationMatch(ctx context.Context, key string, data []byte, contentType string, generation int64) (int64, error) {
	obj := c.client.Bucket(c.bucket).Object(key).If(storage.Conditions{GenerationMatch: generation})
	w := obj.NewWriter(ctx)
	w.ContentType = contentType
	if _, err := w.Write(data); err != nil {
		_ = w.Close()
		return 0, err
	}
	if err := w.Close(); err != nil {
		return 0, fmt.Errorf("precondition failed: %w", err)
	}
	attrs, err := c.client.Bucket(c.bucket).Object(key).Attrs(ctx)
	if err != nil {
		return 0, err
	}
	return attrs.Generation, nil
}

func (c *gcsRealClient) DeleteObject(ctx context.Context, key string) error {
	err := c.client.Bucket(c.bucket).Object(key).Delete(ctx)
	if errors.Is(err, storage.ErrObjectNotExist) {
		return ErrGCSNotFound
	}
	return err
}

func (c *gcsRealClient) ListObjects(ctx context.Context, prefix string) ([]string, error) {
	it := c.client.Bucket(c.bucket).Objects(ctx, &storage.Query{Prefix: prefix})
	var keys []string
	for {
		attrs, err := it.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return nil, err
		}
		keys = append(keys, attrs.Name)
	}
	return keys, nil
}
