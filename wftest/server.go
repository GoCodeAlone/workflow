package wftest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/GoCodeAlone/workflow/module"
	"github.com/gorilla/websocket"
)

// startServer starts the engine (registering trigger routes on the router),
// finds the HTTP router, and wraps it with an httptest.Server. Cleanup is
// registered via t.Cleanup.
func (h *Harness) startServer() {
	h.t.Helper()

	ctx, cancel := context.WithCancel(context.Background())

	if err := h.engine.Start(ctx); err != nil {
		cancel()
		h.t.Fatalf("wftest: engine.Start failed: %v", err)
	}

	// Find the HTTP router in the service registry.
	var router *module.StandardHTTPRouter
	for _, svc := range h.engine.App().SvcRegistry() {
		if r, ok := svc.(*module.StandardHTTPRouter); ok {
			router = r
			break
		}
	}
	if router == nil {
		cancel()
		_ = h.engine.Stop(context.Background())
		h.t.Fatalf("wftest: WithServer requires an http.router module in the config")
	}

	h.httpServer = httptest.NewServer(router)
	h.baseURL = h.httpServer.URL

	h.t.Cleanup(func() {
		h.httpServer.Close()
		cancel()
		_ = h.engine.Stop(context.Background())
	})
}

// BaseURL returns the base URL of the test HTTP server (e.g. "http://127.0.0.1:PORT").
// Panics via t.Fatal if WithServer() was not used.
func (h *Harness) BaseURL() string {
	if h.baseURL == "" {
		h.t.Fatal("wftest: BaseURL requires WithServer() option")
	}
	return h.baseURL
}

// WSDialer opens a WebSocket connection to the given path on the test server.
// Requires WithServer().
func (h *Harness) WSDialer(path string) (*websocket.Conn, *http.Response, error) {
	wsURL := "ws" + strings.TrimPrefix(h.baseURL, "http") + path
	return websocket.DefaultDialer.Dial(wsURL, nil)
}
