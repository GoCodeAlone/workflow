package connector

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
)

// WebhookSource is an EventSource that receives HTTP webhooks and emits
// them as CloudEvents. It wraps the common HTTP trigger pattern used
// elsewhere in the workflow engine.
type WebhookSource struct {
	name    string
	address string
	path    string
	secret  string

	server  *http.Server
	output  chan<- Event
	healthy bool
	mu      sync.RWMutex
}

// NewWebhookSource creates a WebhookSource from a config map.
// Supported config keys: address, path, secret.
func NewWebhookSource(name string, config map[string]any) (*WebhookSource, error) {
	address := ":8080"
	if addr, ok := config["address"].(string); ok && addr != "" {
		address = addr
	}

	path := "/webhook"
	if p, ok := config["path"].(string); ok && p != "" {
		path = p
	}

	secret := ""
	if s, ok := config["secret"].(string); ok {
		secret = s
	}

	return &WebhookSource{
		name:    name,
		address: address,
		path:    path,
		secret:  secret,
	}, nil
}

// Name returns the connector instance name.
func (ws *WebhookSource) Name() string { return ws.name }

// Type returns the connector type identifier.
func (ws *WebhookSource) Type() string { return "webhook" }

// Start begins listening for HTTP webhooks, writing received events to output.
func (ws *WebhookSource) Start(ctx context.Context, output chan<- Event) error {
	ws.mu.Lock()
	ws.output = output
	ws.mu.Unlock()

	mux := http.NewServeMux()
	mux.HandleFunc(ws.path, ws.handleWebhook)

	ws.server = &http.Server{
		Addr:              ws.address,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	ln, err := net.Listen("tcp", ws.address)
	if err != nil {
		return fmt.Errorf("webhook source %q: listen on %s: %w", ws.name, ws.address, err)
	}

	// Store the resolved address (useful when port 0 is used).
	ws.mu.Lock()
	ws.address = ln.Addr().String()
	ws.healthy = true
	ws.mu.Unlock()

	go func() {
		if serveErr := ws.server.Serve(ln); serveErr != nil && serveErr != http.ErrServerClosed {
			ws.mu.Lock()
			ws.healthy = false
			ws.mu.Unlock()
		}
	}()

	return nil
}

// Stop gracefully shuts down the HTTP server.
func (ws *WebhookSource) Stop(ctx context.Context) error {
	ws.mu.Lock()
	ws.healthy = false
	ws.mu.Unlock()

	if ws.server != nil {
		return ws.server.Shutdown(ctx)
	}
	return nil
}

// Healthy returns true when the server is running.
func (ws *WebhookSource) Healthy() bool {
	ws.mu.RLock()
	defer ws.mu.RUnlock()
	return ws.healthy
}

// Checkpoint is a no-op for webhooks (stateless).
func (ws *WebhookSource) Checkpoint(_ context.Context) error {
	return nil
}

// Addr returns the resolved listen address. Useful when the source was
// started on port 0 to let the OS pick a port.
func (ws *WebhookSource) Addr() string {
	ws.mu.RLock()
	defer ws.mu.RUnlock()
	return ws.address
}

// handleWebhook processes an incoming HTTP webhook request.
func (ws *WebhookSource) handleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}
	defer func() { _ = r.Body.Close() }()

	// Validate HMAC signature if a secret is configured.
	if ws.secret != "" {
		sig := r.Header.Get("X-Signature-256")
		if !ws.validateSignature(body, sig) {
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}
	}

	event := Event{
		ID:              uuid.New().String(),
		Source:          "webhook/" + ws.name,
		Type:            r.Header.Get("X-Event-Type"),
		Subject:         r.URL.Path,
		Time:            time.Now().UTC(),
		Data:            json.RawMessage(body),
		DataContentType: r.Header.Get("Content-Type"),
	}
	if event.Type == "" {
		event.Type = "webhook.received"
	}

	ws.mu.RLock()
	output := ws.output
	ws.mu.RUnlock()

	if output != nil {
		select {
		case output <- event:
			w.WriteHeader(http.StatusAccepted)
		default:
			http.Error(w, "output channel full", http.StatusServiceUnavailable)
		}
	} else {
		http.Error(w, "source not started", http.StatusServiceUnavailable)
	}
}

// validateSignature checks an HMAC-SHA256 signature against the body.
func (ws *WebhookSource) validateSignature(body []byte, signature string) bool {
	if signature == "" {
		return false
	}

	// Strip optional "sha256=" prefix.
	const prefix = "sha256="
	if len(signature) > len(prefix) && signature[:len(prefix)] == prefix {
		signature = signature[len(prefix):]
	}

	expected, err := hex.DecodeString(signature)
	if err != nil {
		return false
	}

	mac := hmac.New(sha256.New, []byte(ws.secret))
	mac.Write(body)
	return hmac.Equal(mac.Sum(nil), expected)
}

// WebhookSourceFactory is a SourceFactory for creating WebhookSource instances.
func WebhookSourceFactory(name string, config map[string]any) (EventSource, error) {
	return NewWebhookSource(name, config)
}
