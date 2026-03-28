package module

import (
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"
)

// slogLogger adapts *slog.Logger to the modular.Logger interface for tests.
type slogLogger struct{ l *slog.Logger }

func (s *slogLogger) Info(msg string, args ...any)  { s.l.Info(msg, args...) }
func (s *slogLogger) Error(msg string, args ...any) { s.l.Error(msg, args...) }
func (s *slogLogger) Warn(msg string, args ...any)  { s.l.Warn(msg, args...) }
func (s *slogLogger) Debug(msg string, args ...any) { s.l.Debug(msg, args...) }

func newTestHTTPServer(addr string) *StandardHTTPServer {
	srv := NewStandardHTTPServer("test-srv", addr)
	srv.logger = &slogLogger{slog.Default()}
	mux := http.NewServeMux()
	srv.AddRouter(&muxRouter{mux})
	return srv
}

// TestServerListenError verifies that a fatal ListenAndServe error surfaces on
// the channel returned by ListenError() instead of being silently swallowed.
func TestServerListenError(t *testing.T) {
	// Grab a free port and keep the listener open so the server's bind fails.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to get free port: %v", err)
	}
	addr := ln.Addr().String()
	defer ln.Close()

	srv := newTestHTTPServer(addr)

	if err := srv.Start(t.Context()); err != nil {
		t.Fatalf("Start() unexpected error: %v", err)
	}

	// The listen goroutine should fail quickly and push the error.
	select {
	case listenErr, ok := <-srv.ListenError():
		if !ok {
			t.Fatal("listenErr channel closed without an error (expected EADDRINUSE)")
		}
		if listenErr == nil {
			t.Fatal("expected non-nil error from ListenError()")
		}
		t.Logf("received expected error: %v", listenErr)
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for ListenError()")
	}
}

// TestServerListenErrorChannelClosedOnCleanShutdown verifies that the channel
// is closed after a clean Shutdown so waiters can detect the goroutine exit.
func TestServerListenErrorChannelClosedOnCleanShutdown(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to get free port: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close() // release so the server can bind

	srv := newTestHTTPServer(addr)

	if err := srv.Start(t.Context()); err != nil {
		t.Fatalf("Start() unexpected error: %v", err)
	}

	// Give the goroutine a moment to bind.
	time.Sleep(50 * time.Millisecond)

	if err := srv.Stop(t.Context()); err != nil {
		t.Fatalf("Stop() unexpected error: %v", err)
	}

	// After clean shutdown the channel should be closed with no error.
	select {
	case listenErr, ok := <-srv.ListenError():
		if ok {
			t.Fatalf("expected channel to be closed, got error: %v", listenErr)
		}
		// ok == false → channel closed cleanly.
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for ListenError() channel to close")
	}
}

// muxRouter adapts http.ServeMux to the HTTPRouter interface for testing.
type muxRouter struct{ mux *http.ServeMux }

func (r *muxRouter) AddRoute(method, path string, handler HTTPHandler) {
	r.mux.HandleFunc(path, func(w http.ResponseWriter, req *http.Request) {
		if req.Method != method {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		handler.Handle(w, req)
	})
}

func (r *muxRouter) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mux.ServeHTTP(w, req)
}
