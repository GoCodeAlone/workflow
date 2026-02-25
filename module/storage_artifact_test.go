package module

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// ─── helpers ────────────────────────────────────────────────────────────────

func newTestArtifactFS(t *testing.T) (*ArtifactFSModule, string) {
	t.Helper()
	dir := t.TempDir()
	m := NewArtifactFSModule("test-artifacts", ArtifactFSConfig{BasePath: dir})
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	return m, dir
}

// ─── ArtifactFSModule tests ─────────────────────────────────────────────────

func TestArtifactFS_UploadDownload(t *testing.T) {
	m, _ := newTestArtifactFS(t)
	ctx := context.Background()

	content := []byte("hello artifact world")
	meta := map[string]string{"version": "1.0", "env": "test"}

	if err := m.Upload(ctx, "builds/v1/app.bin", bytes.NewReader(content), meta); err != nil {
		t.Fatalf("Upload: %v", err)
	}

	rc, gotMeta, err := m.Download(ctx, "builds/v1/app.bin")
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	defer rc.Close()

	got, _ := io.ReadAll(rc)
	if string(got) != string(content) {
		t.Errorf("content mismatch: got %q, want %q", got, content)
	}
	if gotMeta["version"] != "1.0" {
		t.Errorf("metadata version: got %q, want 1.0", gotMeta["version"])
	}
	if gotMeta["env"] != "test" {
		t.Errorf("metadata env: got %q, want test", gotMeta["env"])
	}
}

func TestArtifactFS_DownloadNotFound(t *testing.T) {
	m, _ := newTestArtifactFS(t)
	ctx := context.Background()

	_, _, err := m.Download(ctx, "nonexistent/key")
	if err == nil {
		t.Fatal("expected error for missing key")
	}
}

func TestArtifactFS_Exists(t *testing.T) {
	m, _ := newTestArtifactFS(t)
	ctx := context.Background()

	exists, err := m.Exists(ctx, "mykey")
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if exists {
		t.Error("expected false before upload")
	}

	if err := m.Upload(ctx, "mykey", strings.NewReader("data"), nil); err != nil {
		t.Fatalf("Upload: %v", err)
	}

	exists, err = m.Exists(ctx, "mykey")
	if err != nil {
		t.Fatalf("Exists after upload: %v", err)
	}
	if !exists {
		t.Error("expected true after upload")
	}
}

func TestArtifactFS_Delete(t *testing.T) {
	m, dir := newTestArtifactFS(t)
	ctx := context.Background()

	if err := m.Upload(ctx, "to-delete", strings.NewReader("bye"), nil); err != nil {
		t.Fatalf("Upload: %v", err)
	}

	if err := m.Delete(ctx, "to-delete"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	exists, _ := m.Exists(ctx, "to-delete")
	if exists {
		t.Error("artifact should not exist after delete")
	}

	// Sidecar should also be gone.
	metaFile := filepath.Join(dir, "to-delete.meta")
	if _, err := os.Stat(metaFile); !os.IsNotExist(err) {
		t.Error("sidecar metadata file should be removed after delete")
	}
}

func TestArtifactFS_DeleteNotFound(t *testing.T) {
	m, _ := newTestArtifactFS(t)
	ctx := context.Background()

	if err := m.Delete(ctx, "ghost"); err == nil {
		t.Error("expected error deleting nonexistent key")
	}
}

func TestArtifactFS_List(t *testing.T) {
	m, _ := newTestArtifactFS(t)
	ctx := context.Background()

	uploads := []string{
		"builds/v1/app.bin",
		"builds/v1/checksum.txt",
		"builds/v2/app.bin",
		"logs/deploy.log",
	}
	for _, key := range uploads {
		if err := m.Upload(ctx, key, strings.NewReader("x"), nil); err != nil {
			t.Fatalf("Upload %q: %v", key, err)
		}
	}

	// List all.
	all, err := m.List(ctx, "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 4 {
		t.Errorf("expected 4 artifacts, got %d", len(all))
	}

	// List by prefix.
	builds, err := m.List(ctx, "builds/v1/")
	if err != nil {
		t.Fatalf("List builds/v1/: %v", err)
	}
	if len(builds) != 2 {
		t.Errorf("expected 2 builds/v1/ artifacts, got %d", len(builds))
	}

	logs, err := m.List(ctx, "logs/")
	if err != nil {
		t.Fatalf("List logs/: %v", err)
	}
	if len(logs) != 1 {
		t.Errorf("expected 1 log artifact, got %d", len(logs))
	}

	none, err := m.List(ctx, "does-not-exist/")
	if err != nil {
		t.Fatalf("List nonexistent prefix: %v", err)
	}
	if len(none) != 0 {
		t.Errorf("expected 0 artifacts for nonexistent prefix, got %d", len(none))
	}
}

func TestArtifactFS_MaxSize(t *testing.T) {
	dir := t.TempDir()
	m := NewArtifactFSModule("limited", ArtifactFSConfig{BasePath: dir, MaxSize: 5})
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	// Upload within limit.
	if err := m.Upload(ctx, "small", strings.NewReader("abc"), nil); err != nil {
		t.Fatalf("Upload small: %v", err)
	}

	// Upload exceeding limit.
	err := m.Upload(ctx, "large", strings.NewReader("toolarge"), nil)
	if err == nil {
		t.Error("expected error for oversized artifact")
	}
	if !strings.Contains(err.Error(), "exceeds limit") {
		t.Errorf("error should mention exceeds limit: %v", err)
	}
}

func TestArtifactFS_ConcurrentAccess(t *testing.T) {
	m, _ := newTestArtifactFS(t)
	ctx := context.Background()

	const workers = 10
	var wg sync.WaitGroup

	// Concurrent uploads.
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			key := strings.Repeat("a", id+1) // unique keys
			content := strings.Repeat("x", (id+1)*100)
			if err := m.Upload(ctx, key, strings.NewReader(content), nil); err != nil {
				t.Errorf("concurrent Upload[%d]: %v", id, err)
			}
		}(i)
	}
	wg.Wait()

	// Concurrent downloads and lists.
	for i := 0; i < workers; i++ {
		wg.Add(2)
		go func(id int) {
			defer wg.Done()
			key := strings.Repeat("a", id+1)
			rc, _, err := m.Download(ctx, key)
			if err != nil {
				t.Errorf("concurrent Download[%d]: %v", id, err)
				return
			}
			rc.Close()
		}(i)
		go func() {
			defer wg.Done()
			_, _ = m.List(ctx, "")
		}()
	}
	wg.Wait()
}

// ─── pipeline step tests ─────────────────────────────────────────────────────

// mockArtifactStore is an in-memory ArtifactStore for testing pipeline steps.
type mockArtifactStore struct {
	mu       sync.RWMutex
	data     map[string][]byte
	metadata map[string]map[string]string
	putErr   error
	getErr   error
}

func newMockArtifactStore() *mockArtifactStore {
	return &mockArtifactStore{
		data:     make(map[string][]byte),
		metadata: make(map[string]map[string]string),
	}
}

func (s *mockArtifactStore) Upload(_ context.Context, key string, reader io.Reader, md map[string]string) error {
	if s.putErr != nil {
		return s.putErr
	}
	data, _ := io.ReadAll(reader)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[key] = data
	s.metadata[key] = md
	return nil
}

func (s *mockArtifactStore) Download(_ context.Context, key string) (io.ReadCloser, map[string]string, error) {
	if s.getErr != nil {
		return nil, nil, s.getErr
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	data, ok := s.data[key]
	if !ok {
		return nil, nil, io.ErrUnexpectedEOF
	}
	return io.NopCloser(bytes.NewReader(data)), s.metadata[key], nil
}

func (s *mockArtifactStore) List(_ context.Context, prefix string) ([]ArtifactInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var results []ArtifactInfo
	for k, data := range s.data {
		if prefix == "" || strings.HasPrefix(k, prefix) {
			results = append(results, ArtifactInfo{Key: k, Size: int64(len(data))})
		}
	}
	return results, nil
}

func (s *mockArtifactStore) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, key)
	delete(s.metadata, key)
	return nil
}

func (s *mockArtifactStore) Exists(_ context.Context, key string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.data[key]
	return ok, nil
}

func mockAppWithArtifactStore(name string, store ArtifactStore) *MockApplication {
	app := NewMockApplication()
	app.Services[name] = store
	return app
}

func TestArtifactUploadStep_Basic(t *testing.T) {
	store := newMockArtifactStore()
	app := mockAppWithArtifactStore("artifacts", store)

	// Write a temp source file.
	tmpFile := filepath.Join(t.TempDir(), "app.bin")
	if err := os.WriteFile(tmpFile, []byte("binary content"), 0o640); err != nil {
		t.Fatal(err)
	}

	factory := NewArtifactUploadStepFactory()
	step, err := factory("upload-app", map[string]any{
		"store":  "artifacts",
		"key":    "builds/v1/app.bin",
		"source": tmpFile,
		"metadata": map[string]any{
			"version": "1.0",
		},
	}, app)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if result.Output["key"] != "builds/v1/app.bin" {
		t.Errorf("key: got %v", result.Output["key"])
	}

	data, ok := store.data["builds/v1/app.bin"]
	if !ok {
		t.Fatal("artifact not stored")
	}
	if string(data) != "binary content" {
		t.Errorf("stored content mismatch: %q", data)
	}
}

func TestArtifactUploadStep_MissingRequiredConfig(t *testing.T) {
	factory := NewArtifactUploadStepFactory()

	_, err := factory("x", map[string]any{"key": "k", "source": "s"}, nil)
	if err == nil {
		t.Error("expected error for missing store")
	}
	_, err = factory("x", map[string]any{"store": "s", "source": "f"}, nil)
	if err == nil {
		t.Error("expected error for missing key")
	}
	_, err = factory("x", map[string]any{"store": "s", "key": "k"}, nil)
	if err == nil {
		t.Error("expected error for missing source")
	}
}

func TestArtifactDownloadStep_Basic(t *testing.T) {
	store := newMockArtifactStore()
	store.data["builds/v1/app.bin"] = []byte("downloaded content")
	store.metadata["builds/v1/app.bin"] = map[string]string{"version": "1.0"}
	app := mockAppWithArtifactStore("artifacts", store)

	destDir := t.TempDir()
	destFile := filepath.Join(destDir, "app.bin")

	factory := NewArtifactDownloadStepFactory()
	step, err := factory("download-app", map[string]any{
		"store": "artifacts",
		"key":   "builds/v1/app.bin",
		"dest":  destFile,
	}, app)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if result.Output["dest"] != destFile {
		t.Errorf("dest: got %v", result.Output["dest"])
	}

	content, err := os.ReadFile(destFile)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(content) != "downloaded content" {
		t.Errorf("content mismatch: %q", content)
	}
}

func TestArtifactListStep_Basic(t *testing.T) {
	store := newMockArtifactStore()
	store.data["builds/v1/app.bin"] = []byte("a")
	store.data["builds/v2/app.bin"] = []byte("b")
	store.data["logs/run.log"] = []byte("c")
	app := mockAppWithArtifactStore("artifacts", store)

	factory := NewArtifactListStepFactory()
	step, err := factory("list-builds", map[string]any{
		"store":  "artifacts",
		"prefix": "builds/",
	}, app)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	items, _ := result.Output["artifacts"].([]map[string]any)
	if len(items) != 2 {
		t.Errorf("expected 2 builds artifacts, got %d", len(items))
	}
	if result.Output["count"] != 2 {
		t.Errorf("count: got %v", result.Output["count"])
	}
}

func TestArtifactDeleteStep_Basic(t *testing.T) {
	store := newMockArtifactStore()
	store.data["to-remove"] = []byte("bye")
	app := mockAppWithArtifactStore("artifacts", store)

	factory := NewArtifactDeleteStepFactory()
	step, err := factory("del", map[string]any{
		"store": "artifacts",
		"key":   "to-remove",
	}, app)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if result.Output["deleted"] != true {
		t.Errorf("expected deleted=true, got %v", result.Output["deleted"])
	}

	if _, ok := store.data["to-remove"]; ok {
		t.Error("artifact still present after delete")
	}
}

func TestArtifactSteps_StoreNotFound(t *testing.T) {
	app := NewMockApplication() // no services registered
	ctx := context.Background()
	pc := NewPipelineContext(nil, nil)

	for _, tc := range []struct {
		name    string
		factory StepFactory
		cfg     map[string]any
	}{
		{"upload", NewArtifactUploadStepFactory(), map[string]any{"store": "missing", "key": "k", "source": "s"}},
		{"download", NewArtifactDownloadStepFactory(), map[string]any{"store": "missing", "key": "k", "dest": "d"}},
		{"list", NewArtifactListStepFactory(), map[string]any{"store": "missing"}},
		{"delete", NewArtifactDeleteStepFactory(), map[string]any{"store": "missing", "key": "k"}},
	} {
		step, err := tc.factory(tc.name, tc.cfg, app)
		if err != nil {
			t.Fatalf("%s factory: %v", tc.name, err)
		}
		_, err = step.Execute(ctx, pc)
		if err == nil {
			t.Errorf("%s: expected error for missing store service", tc.name)
		}
	}
}
