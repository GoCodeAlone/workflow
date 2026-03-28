package module

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
)

// TestTrackedResponseWriter verifies the atomic.Bool + sync.Once semantics.
func TestTrackedResponseWriter(t *testing.T) {
	rec := httptest.NewRecorder()
	rw := &trackedResponseWriter{ResponseWriter: rec}

	if rw.written.Load() {
		t.Fatal("written should be false before any write")
	}

	rw.WriteHeader(http.StatusAccepted)

	if !rw.written.Load() {
		t.Fatal("written should be true after WriteHeader")
	}
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected status 202, got %d", rec.Code)
	}

	// A second WriteHeader must be absorbed by sync.Once.
	rw.WriteHeader(http.StatusInternalServerError)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("second WriteHeader must not change status; got %d", rec.Code)
	}
}

// TestTrackedResponseWriterWriteSetsFlag verifies that Write() sets the flag.
func TestTrackedResponseWriterWriteSetsFlag(t *testing.T) {
	rec := httptest.NewRecorder()
	rw := &trackedResponseWriter{ResponseWriter: rec}

	n, err := rw.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	if n != 5 {
		t.Fatalf("expected 5 bytes written, got %d", n)
	}
	if !rw.written.Load() {
		t.Fatal("written should be true after Write")
	}
}

// safeResponseWriter is a thread-safe http.ResponseWriter for race-detector tests.
// It only records how many times WriteHeader and Write are called, without
// touching any unsynchronized state.
type safeResponseWriter struct {
	mu          sync.Mutex
	headerCalls atomic.Int32
	writeCalls  atomic.Int32
	headers     http.Header
}

func newSafeResponseWriter() *safeResponseWriter {
	return &safeResponseWriter{headers: make(http.Header)}
}

func (s *safeResponseWriter) Header() http.Header {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.headers
}

func (s *safeResponseWriter) Write(b []byte) (int, error) {
	s.writeCalls.Add(1)
	return len(b), nil
}

func (s *safeResponseWriter) WriteHeader(code int) {
	s.headerCalls.Add(1)
}

// TestTrackedResponseWriterConcurrent verifies no data race under the -race
// detector when multiple goroutines call WriteHeader concurrently.
// sync.Once must ensure WriteHeader propagates to the underlying writer exactly once.
func TestTrackedResponseWriterConcurrent(t *testing.T) {
	const workers = 50

	base := newSafeResponseWriter()
	rw := &trackedResponseWriter{ResponseWriter: base}

	var wg sync.WaitGroup
	for i := range workers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				rw.WriteHeader(http.StatusOK)
			} else {
				_, _ = rw.Write([]byte("x"))
			}
		}(i)
	}
	wg.Wait()

	// The written flag must be set.
	if !rw.written.Load() {
		t.Fatal("written should be true after concurrent writes")
	}

	// WriteHeader must have been forwarded to the underlying writer exactly once.
	if got := base.headerCalls.Load(); got != 1 {
		t.Fatalf("expected underlying WriteHeader called once, got %d", got)
	}
}
