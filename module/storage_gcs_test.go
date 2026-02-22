package module

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"cloud.google.com/go/storage"
	"google.golang.org/api/iterator"
)

// ---- mock helpers ----

// mockObjectIterator returns a fixed list of attrs, then iterator.Done.
type mockObjectIterator struct {
	items []*storage.ObjectAttrs
	pos   int
	err   error // returned after all items
}

func (m *mockObjectIterator) Next() (*storage.ObjectAttrs, error) {
	if m.err != nil && m.pos >= len(m.items) {
		return nil, m.err
	}
	if m.pos >= len(m.items) {
		return nil, iterator.Done
	}
	a := m.items[m.pos]
	m.pos++
	return a, nil
}

// mockObjectHandle simulates a single GCS object.
type mockObjectHandle struct {
	content   []byte
	attrs     *storage.ObjectAttrs
	readErr   error
	writeErr  error
	deleteErr error
	attrsErr  error
	written   *bytes.Buffer // captures Put data
}

func (m *mockObjectHandle) NewReader(_ context.Context) (io.ReadCloser, error) {
	if m.readErr != nil {
		return nil, m.readErr
	}
	return io.NopCloser(bytes.NewReader(m.content)), nil
}

func (m *mockObjectHandle) NewWriter(_ context.Context) io.WriteCloser {
	m.written = &bytes.Buffer{}
	return &mockWriteCloser{buf: m.written, closeErr: m.writeErr}
}

func (m *mockObjectHandle) Delete(_ context.Context) error { return m.deleteErr }

func (m *mockObjectHandle) Attrs(_ context.Context) (*storage.ObjectAttrs, error) {
	if m.attrsErr != nil {
		return nil, m.attrsErr
	}
	return m.attrs, nil
}

// mockWriteCloser is an io.WriteCloser backed by a bytes.Buffer.
type mockWriteCloser struct {
	buf      *bytes.Buffer
	closeErr error
}

func (w *mockWriteCloser) Write(p []byte) (int, error) { return w.buf.Write(p) }
func (w *mockWriteCloser) Close() error                { return w.closeErr }

// mockBucketHandle routes Object() calls to per-key mockObjectHandles.
type mockBucketHandle struct {
	objects map[string]*mockObjectHandle
	listErr error
	listItems []*storage.ObjectAttrs
}

func (m *mockBucketHandle) Objects(_ context.Context, _ *storage.Query) objectIterator {
	return &mockObjectIterator{items: m.listItems, err: m.listErr}
}

func (m *mockBucketHandle) Object(name string) objectHandle {
	if oh, ok := m.objects[name]; ok {
		return oh
	}
	return &mockObjectHandle{attrsErr: fmt.Errorf("object %q not found", name),
		readErr:   fmt.Errorf("object %q not found", name),
		deleteErr: fmt.Errorf("object %q not found", name),
	}
}

// newGCSWithMock wires up a GCSStorage backed by a mockBucketHandle.
func newGCSWithMock(bh gcsBucketHandle) *GCSStorage {
	g := NewGCSStorage("gcs-test")
	g.setBucketHandle(bh)
	return g
}

// ---- existing basic tests ----

func TestGCSStorageName(t *testing.T) {
	g := NewGCSStorage("gcs-test")
	if g.Name() != "gcs-test" {
		t.Errorf("expected name 'gcs-test', got %q", g.Name())
	}
}

func TestGCSStorageModuleInterface(t *testing.T) {
	g := NewGCSStorage("gcs-test")

	// Test Init
	app, _ := NewTestApplication()
	if err := g.Init(app); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Test ProvidesServices
	services := g.ProvidesServices()
	if len(services) != 1 {
		t.Fatalf("expected 1 service, got %d", len(services))
	}
	if services[0].Name != "gcs-test" {
		t.Errorf("expected service name 'gcs-test', got %q", services[0].Name)
	}

	// Test RequiresServices
	deps := g.RequiresServices()
	if len(deps) != 0 {
		t.Errorf("expected no dependencies, got %d", len(deps))
	}
}

func TestGCSStorageConfig(t *testing.T) {
	g := NewGCSStorage("gcs-test")

	g.SetBucket("my-bucket")
	if g.bucket != "my-bucket" {
		t.Errorf("expected bucket 'my-bucket', got %q", g.bucket)
	}

	g.SetProject("my-project")
	if g.project != "my-project" {
		t.Errorf("expected project 'my-project', got %q", g.project)
	}

	g.SetCredentialsFile("/path/to/creds.json")
	if g.credentialsFile != "/path/to/creds.json" {
		t.Errorf("expected credentialsFile '/path/to/creds.json', got %q", g.credentialsFile)
	}
}

func TestGCSStorageOperationsWithoutClient(t *testing.T) {
	g := NewGCSStorage("gcs-test")

	ctx := context.Background()

	// Operations should fail without Start
	if _, err := g.List(ctx, ""); err == nil {
		t.Error("List should fail without initialized client")
	}

	if _, err := g.Get(ctx, "key"); err == nil {
		t.Error("Get should fail without initialized client")
	}

	if err := g.Put(ctx, "key", nil); err == nil {
		t.Error("Put should fail without initialized client")
	}

	if err := g.Delete(ctx, "key"); err == nil {
		t.Error("Delete should fail without initialized client")
	}

	if _, err := g.Stat(ctx, "key"); err == nil {
		t.Error("Stat should fail without initialized client")
	}
}

func TestGCSStorageStop(t *testing.T) {
	g := NewGCSStorage("gcs-test")
	app, _ := NewTestApplication()
	_ = g.Init(app)

	// Stop without Start should be safe (no-op when client is nil)
	if err := g.Stop(context.Background()); err != nil {
		t.Fatalf("Stop without Start failed: %v", err)
	}
}

func TestGCSStorageMkdirAll(t *testing.T) {
	g := NewGCSStorage("gcs-test")

	// MkdirAll is a no-op for object storage
	if err := g.MkdirAll(context.Background(), "some/path"); err != nil {
		t.Fatalf("MkdirAll should be a no-op, got error: %v", err)
	}
}

// ---- mock-based tests ----

func TestGCSStorage_List(t *testing.T) {
	now := time.Now()
	bh := &mockBucketHandle{
		listItems: []*storage.ObjectAttrs{
			{Name: "prefix/a.txt", Size: 10, Updated: now},
			{Name: "prefix/b.txt", Size: 20, Updated: now},
		},
	}
	g := newGCSWithMock(bh)

	files, err := g.List(context.Background(), "prefix/")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(files))
	}
	if files[0].Name != "prefix/a.txt" || files[0].Size != 10 {
		t.Errorf("unexpected first file: %+v", files[0])
	}
	if files[1].Name != "prefix/b.txt" || files[1].Size != 20 {
		t.Errorf("unexpected second file: %+v", files[1])
	}
	if !files[0].ModTime.Equal(now) {
		t.Errorf("ModTime mismatch: got %v, want %v", files[0].ModTime, now)
	}
}

func TestGCSStorage_ListEmpty(t *testing.T) {
	bh := &mockBucketHandle{}
	g := newGCSWithMock(bh)

	files, err := g.List(context.Background(), "none/")
	if err != nil {
		t.Fatalf("List empty: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("expected 0 files, got %d", len(files))
	}
}

func TestGCSStorage_ListError(t *testing.T) {
	bh := &mockBucketHandle{
		listItems: []*storage.ObjectAttrs{{Name: "a.txt", Size: 1}},
		listErr:   fmt.Errorf("permission denied"),
	}
	g := newGCSWithMock(bh)

	_, err := g.List(context.Background(), "")
	if err == nil {
		t.Fatal("expected error from iterator, got nil")
	}
	if !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestGCSStorage_Get(t *testing.T) {
	content := []byte("hello gcs")
	bh := &mockBucketHandle{
		objects: map[string]*mockObjectHandle{
			"myfile.txt": {content: content},
		},
	}
	g := newGCSWithMock(bh)

	rc, err := g.Get(context.Background(), "myfile.txt")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer rc.Close()

	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("content mismatch: got %q, want %q", got, content)
	}
}

func TestGCSStorage_GetNotFound(t *testing.T) {
	bh := &mockBucketHandle{objects: map[string]*mockObjectHandle{}}
	g := newGCSWithMock(bh)

	_, err := g.Get(context.Background(), "missing.txt")
	if err == nil {
		t.Fatal("expected error for missing object, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestGCSStorage_Put(t *testing.T) {
	oh := &mockObjectHandle{}
	bh := &mockBucketHandle{objects: map[string]*mockObjectHandle{"upload.txt": oh}}
	g := newGCSWithMock(bh)

	data := []byte("data to upload")
	if err := g.Put(context.Background(), "upload.txt", bytes.NewReader(data)); err != nil {
		t.Fatalf("Put: %v", err)
	}

	if oh.written == nil {
		t.Fatal("expected data to be written, but writer was never used")
	}
	if !bytes.Equal(oh.written.Bytes(), data) {
		t.Errorf("written data mismatch: got %q, want %q", oh.written.Bytes(), data)
	}
}

func TestGCSStorage_PutWriteError(t *testing.T) {
	oh := &mockObjectHandle{writeErr: fmt.Errorf("write failed")}
	bh := &mockBucketHandle{objects: map[string]*mockObjectHandle{"bad.txt": oh}}
	g := newGCSWithMock(bh)

	err := g.Put(context.Background(), "bad.txt", bytes.NewReader([]byte("x")))
	if err == nil {
		t.Fatal("expected write error, got nil")
	}
	if !strings.Contains(err.Error(), "write failed") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestGCSStorage_Delete(t *testing.T) {
	oh := &mockObjectHandle{}
	bh := &mockBucketHandle{objects: map[string]*mockObjectHandle{"del.txt": oh}}
	g := newGCSWithMock(bh)

	if err := g.Delete(context.Background(), "del.txt"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
}

func TestGCSStorage_DeleteNotFound(t *testing.T) {
	bh := &mockBucketHandle{objects: map[string]*mockObjectHandle{}}
	g := newGCSWithMock(bh)

	err := g.Delete(context.Background(), "ghost.txt")
	if err == nil {
		t.Fatal("expected error for missing object, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestGCSStorage_Stat(t *testing.T) {
	now := time.Now()
	oh := &mockObjectHandle{
		attrs: &storage.ObjectAttrs{Name: "stat.txt", Size: 42, Updated: now},
	}
	bh := &mockBucketHandle{objects: map[string]*mockObjectHandle{"stat.txt": oh}}
	g := newGCSWithMock(bh)

	info, err := g.Stat(context.Background(), "stat.txt")
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Name != "stat.txt" {
		t.Errorf("Name: got %q, want 'stat.txt'", info.Name)
	}
	if info.Size != 42 {
		t.Errorf("Size: got %d, want 42", info.Size)
	}
	if !info.ModTime.Equal(now) {
		t.Errorf("ModTime mismatch: got %v, want %v", info.ModTime, now)
	}
}

func TestGCSStorage_StatNotFound(t *testing.T) {
	bh := &mockBucketHandle{objects: map[string]*mockObjectHandle{}}
	g := newGCSWithMock(bh)

	_, err := g.Stat(context.Background(), "noexist.txt")
	if err == nil {
		t.Fatal("expected error for missing object, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("unexpected error: %v", err)
	}
}

