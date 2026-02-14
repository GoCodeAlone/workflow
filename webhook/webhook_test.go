package webhook

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestDefaultRetryConfig(t *testing.T) {
	cfg := DefaultRetryConfig()
	if cfg.MaxRetries != 5 {
		t.Errorf("expected MaxRetries 5, got %d", cfg.MaxRetries)
	}
	if cfg.JitterFraction != 0.1 {
		t.Errorf("expected JitterFraction 0.1, got %f", cfg.JitterFraction)
	}
}

func TestRetryManager_SendSuccess(t *testing.T) {
	var received atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	store := NewDeadLetterStore()
	rm := NewRetryManager(RetryConfig{
		MaxRetries:        3,
		InitialBackoff:    time.Millisecond,
		MaxBackoff:        10 * time.Millisecond,
		BackoffMultiplier: 2.0,
		Timeout:           5 * time.Second,
	}, store)

	d, err := rm.Send(context.Background(), srv.URL, []byte(`{"ok":true}`), map[string]string{"X-Test": "1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.Status != StatusDelivered {
		t.Errorf("expected delivered, got %s", d.Status)
	}
	if d.Attempts != 1 {
		t.Errorf("expected 1 attempt, got %d", d.Attempts)
	}
	if d.DeliveredAt == nil {
		t.Error("expected DeliveredAt to be set")
	}
	if store.Count() != 0 {
		t.Errorf("expected empty dead letter store, got %d", store.Count())
	}
}

func TestRetryManager_RetryThenSucceed(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		if n <= 2 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	store := NewDeadLetterStore()
	rm := NewRetryManager(RetryConfig{
		MaxRetries:        4,
		InitialBackoff:    time.Millisecond,
		MaxBackoff:        5 * time.Millisecond,
		BackoffMultiplier: 2.0,
		Timeout:           5 * time.Second,
	}, store)

	d, err := rm.Send(context.Background(), srv.URL, []byte(`{}`), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.Status != StatusDelivered {
		t.Errorf("expected delivered, got %s", d.Status)
	}
	if d.Attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", d.Attempts)
	}
}

func TestRetryManager_AllRetriesFail_DeadLetter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	store := NewDeadLetterStore()
	rm := NewRetryManager(RetryConfig{
		MaxRetries:        2,
		InitialBackoff:    time.Millisecond,
		MaxBackoff:        5 * time.Millisecond,
		BackoffMultiplier: 2.0,
		Timeout:           5 * time.Second,
	}, store)

	d, err := rm.Send(context.Background(), srv.URL, []byte(`{}`), nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if d.Status != StatusDeadLetter {
		t.Errorf("expected dead_letter, got %s", d.Status)
	}
	if d.Attempts != 3 { // 1 initial + 2 retries
		t.Errorf("expected 3 attempts, got %d", d.Attempts)
	}
	if store.Count() != 1 {
		t.Errorf("expected 1 dead letter, got %d", store.Count())
	}
}

func TestRetryManager_ContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	store := NewDeadLetterStore()
	rm := NewRetryManager(RetryConfig{
		MaxRetries:        10,
		InitialBackoff:    time.Second,
		MaxBackoff:        10 * time.Second,
		BackoffMultiplier: 2.0,
		Timeout:           5 * time.Second,
	}, store)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	d, err := rm.Send(ctx, srv.URL, []byte(`{}`), nil)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
	if d.Status != StatusDeadLetter {
		t.Errorf("expected dead_letter, got %s", d.Status)
	}
}

func TestRetryManager_Replay(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		if n <= 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	store := NewDeadLetterStore()
	rm := NewRetryManager(RetryConfig{
		MaxRetries:        0,
		InitialBackoff:    time.Millisecond,
		MaxBackoff:        5 * time.Millisecond,
		BackoffMultiplier: 2.0,
		Timeout:           5 * time.Second,
	}, store)
	rm.config.MaxRetries = 0

	// First send fails
	d, _ := rm.Send(context.Background(), srv.URL, []byte(`{}`), nil)
	if d.Status != StatusDeadLetter {
		t.Fatalf("expected dead_letter, got %s", d.Status)
	}
	id := d.ID

	// Replay succeeds (server now returns 200)
	rm.config.MaxRetries = 1
	replayed, err := rm.Replay(context.Background(), id)
	if err != nil {
		t.Fatalf("replay failed: %v", err)
	}
	if replayed.Status != StatusDelivered {
		t.Errorf("expected delivered, got %s", replayed.Status)
	}
	if store.Count() != 0 {
		t.Errorf("expected empty store after replay, got %d", store.Count())
	}
}

func TestRetryManager_ReplayNotFound(t *testing.T) {
	store := NewDeadLetterStore()
	rm := NewRetryManager(DefaultRetryConfig(), store)
	_, err := rm.Replay(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent id")
	}
}

func TestRetryManager_BackoffWithJitter(t *testing.T) {
	store := NewDeadLetterStore()
	rm := NewRetryManager(RetryConfig{
		MaxRetries:        3,
		InitialBackoff:    100 * time.Millisecond,
		MaxBackoff:        time.Second,
		BackoffMultiplier: 2.0,
		JitterFraction:    0.5,
		Timeout:           5 * time.Second,
	}, store)

	// Verify backoff values are in reasonable range
	for attempt := 1; attempt <= 5; attempt++ {
		d := rm.backoff(attempt)
		if d < 0 {
			t.Errorf("attempt %d: negative backoff %v", attempt, d)
		}
		// With 50% jitter, max could be 1.5x the base, capped at MaxBackoff * 1.5
		maxExpected := time.Duration(float64(rm.config.MaxBackoff) * 1.5)
		if d > maxExpected {
			t.Errorf("attempt %d: backoff %v exceeds max %v", attempt, d, maxExpected)
		}
	}
}

func TestDeadLetterStore_CRUD(t *testing.T) {
	store := NewDeadLetterStore()

	d1 := &Delivery{ID: "d1", URL: "http://a", Status: StatusDeadLetter, CreatedAt: time.Now().Add(-time.Hour)}
	d2 := &Delivery{ID: "d2", URL: "http://b", Status: StatusDeadLetter, CreatedAt: time.Now()}

	store.Add(d1)
	store.Add(d2)

	if store.Count() != 2 {
		t.Fatalf("expected 2, got %d", store.Count())
	}

	got, ok := store.Get("d1")
	if !ok || got.URL != "http://a" {
		t.Error("Get d1 failed")
	}

	_, ok = store.Get("d3")
	if ok {
		t.Error("expected not found for d3")
	}

	list := store.List()
	if len(list) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(list))
	}
	// Newest first
	if list[0].ID != "d2" {
		t.Errorf("expected d2 first, got %s", list[0].ID)
	}

	removed, ok := store.Remove("d1")
	if !ok || removed.ID != "d1" {
		t.Error("Remove d1 failed")
	}
	if store.Count() != 1 {
		t.Errorf("expected 1 after remove, got %d", store.Count())
	}
}

func TestDeadLetterStore_Purge(t *testing.T) {
	store := NewDeadLetterStore()
	store.Add(&Delivery{ID: "a", CreatedAt: time.Now()})
	store.Add(&Delivery{ID: "b", CreatedAt: time.Now()})
	n := store.Purge()
	if n != 2 {
		t.Errorf("expected 2 purged, got %d", n)
	}
	if store.Count() != 0 {
		t.Errorf("expected 0 after purge, got %d", store.Count())
	}
}

func TestDeadLetterStore_Stats(t *testing.T) {
	store := NewDeadLetterStore()
	now := time.Now()
	store.Add(&Delivery{ID: "a", Status: StatusDeadLetter, Attempts: 3, CreatedAt: now.Add(-2 * time.Hour)})
	store.Add(&Delivery{ID: "b", Status: StatusDeadLetter, Attempts: 5, CreatedAt: now.Add(-time.Hour)})

	stats := store.Stats()
	if stats.Total != 2 {
		t.Errorf("expected 2 total, got %d", stats.Total)
	}
	if stats.TotalRetries != 8 {
		t.Errorf("expected 8 total retries, got %d", stats.TotalRetries)
	}
	if stats.ByStatus["dead_letter"] != 2 {
		t.Errorf("expected 2 dead_letter, got %d", stats.ByStatus["dead_letter"])
	}
}

// --- HTTP handler tests ---

func TestHandler_ListDeadLetters(t *testing.T) {
	store := NewDeadLetterStore()
	store.Add(&Delivery{ID: "x1", URL: "http://test", Status: StatusDeadLetter, CreatedAt: time.Now()})
	rm := NewRetryManager(DefaultRetryConfig(), store)
	h := NewHandler(store, rm)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/webhooks/dead-letter", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	items := resp["items"].([]any)
	if len(items) != 1 {
		t.Errorf("expected 1 item, got %d", len(items))
	}
}

func TestHandler_Stats(t *testing.T) {
	store := NewDeadLetterStore()
	store.Add(&Delivery{ID: "s1", Status: StatusDeadLetter, Attempts: 2, CreatedAt: time.Now()})
	rm := NewRetryManager(DefaultRetryConfig(), store)
	h := NewHandler(store, rm)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/webhooks/dead-letter/stats", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var stats DeadLetterStats
	if err := json.NewDecoder(rec.Body).Decode(&stats); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if stats.Total != 1 {
		t.Errorf("expected 1 total, got %d", stats.Total)
	}
}

func TestHandler_RetryDeadLetter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	store := NewDeadLetterStore()
	store.Add(&Delivery{
		ID:        "retry1",
		URL:       srv.URL,
		Payload:   []byte(`{}`),
		Status:    StatusDeadLetter,
		CreatedAt: time.Now(),
	})

	rm := NewRetryManager(RetryConfig{
		MaxRetries:        1,
		InitialBackoff:    time.Millisecond,
		MaxBackoff:        5 * time.Millisecond,
		BackoffMultiplier: 2.0,
		Timeout:           5 * time.Second,
	}, store)
	h := NewHandler(store, rm)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest("POST", "/api/webhooks/dead-letter/retry1/retry", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if store.Count() != 0 {
		t.Errorf("expected 0 in store after replay, got %d", store.Count())
	}
}

func TestHandler_DeleteDeadLetter(t *testing.T) {
	store := NewDeadLetterStore()
	store.Add(&Delivery{ID: "del1", CreatedAt: time.Now()})
	rm := NewRetryManager(DefaultRetryConfig(), store)
	h := NewHandler(store, rm)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest("DELETE", "/api/webhooks/dead-letter/del1", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if store.Count() != 0 {
		t.Errorf("expected 0, got %d", store.Count())
	}
}

func TestHandler_PurgeDeadLetters(t *testing.T) {
	store := NewDeadLetterStore()
	store.Add(&Delivery{ID: "p1", CreatedAt: time.Now()})
	store.Add(&Delivery{ID: "p2", CreatedAt: time.Now()})
	rm := NewRetryManager(DefaultRetryConfig(), store)
	h := NewHandler(store, rm)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest("DELETE", "/api/webhooks/dead-letter", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if store.Count() != 0 {
		t.Errorf("expected 0 after purge, got %d", store.Count())
	}
}

func TestHandler_RetryNotFound(t *testing.T) {
	store := NewDeadLetterStore()
	rm := NewRetryManager(DefaultRetryConfig(), store)
	h := NewHandler(store, rm)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest("POST", "/api/webhooks/dead-letter/nonexistent/retry", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}
