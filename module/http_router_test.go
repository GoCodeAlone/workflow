package module

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// slowHTTPHandler is an HTTPHandler that sleeps to simulate slow pipeline work.
type slowHTTPHandler struct {
	delay time.Duration
}

func (h *slowHTTPHandler) Handle(w http.ResponseWriter, r *http.Request) {
	time.Sleep(h.delay)
	w.WriteHeader(http.StatusOK)
}

// panicHTTPHandler is an HTTPHandler that always panics.
type panicHTTPHandler struct{}

func (h *panicHTTPHandler) Handle(w http.ResponseWriter, r *http.Request) {
	panic("intentional test panic")
}

// TestRouterConcurrentAddRoute verifies that AddRouteWithMiddleware does not
// deadlock or time out when called while concurrent requests are being served.
// The RLock in ServeHTTP must be released before handler dispatch so that the
// write lock in AddRouteWithMiddleware can proceed.
func TestRouterConcurrentAddRoute(t *testing.T) {
	router := NewStandardHTTPRouter("test-router")
	router.AddRoute("GET", "/slow", &slowHTTPHandler{delay: 20 * time.Millisecond})

	// Start mux
	if err := router.Start(t.Context()); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(router)
	defer srv.Close()

	const concurrency = 100
	var wg sync.WaitGroup
	wg.Add(concurrency)

	// Fire 100 concurrent slow requests.
	for range concurrency {
		go func() {
			defer wg.Done()
			resp, err := srv.Client().Get(srv.URL + "/slow")
			if err != nil {
				return
			}
			resp.Body.Close()
		}()
	}

	// While requests are in-flight, dynamically add routes — this requires the
	// write lock and must not deadlock.
	for i := range 10 {
		router.AddRouteWithMiddleware("GET", fmt.Sprintf("/dynamic/%d", i),
			&slowHTTPHandler{delay: 0}, nil)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// success
	case <-time.After(5 * time.Second):
		t.Fatal("deadlock: concurrent requests did not complete within 5s")
	}
}

// TestPanicRecoveryHTTPHandler verifies that a panicking handler returns 500
// and does not crash the server.
func TestPanicRecoveryHTTPHandler(t *testing.T) {
	router := NewStandardHTTPRouter("panic-router")
	router.AddRoute("GET", "/panic", &panicHTTPHandler{})
	router.AddRoute("GET", "/ok", &slowHTTPHandler{delay: 0})

	if err := router.Start(t.Context()); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(router)
	defer srv.Close()

	// The panicking endpoint should return 500.
	resp, err := srv.Client().Get(srv.URL + "/panic")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", resp.StatusCode)
	}

	// The server must still be alive and serve subsequent requests.
	resp2, err := srv.Client().Get(srv.URL + "/ok")
	if err != nil {
		t.Fatalf("server dead after panic: %v", err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Errorf("expected 200 after recovery, got %d", resp2.StatusCode)
	}
}
