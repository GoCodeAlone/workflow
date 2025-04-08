package module

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/GoCodeAlone/modular"
)

// IntegrationConnector represents a connector to a third-party service
type IntegrationConnector interface {
	// Connect establishes a connection to the external service
	Connect(ctx context.Context) error

	// Disconnect closes the connection to the external service
	Disconnect(ctx context.Context) error

	// Execute performs an action on the external service
	Execute(ctx context.Context, action string, params map[string]interface{}) (map[string]interface{}, error)

	// GetName returns the name of the connector
	GetName() string

	// IsConnected checks if the connector is connected
	IsConnected() bool
}

// HTTPIntegrationConnector implements a connector using HTTP requests
type HTTPIntegrationConnector struct {
	name        string
	baseURL     string
	headers     map[string]string
	authType    string
	authToken   string
	username    string
	password    string
	client      *http.Client
	connected   bool
	timeout     time.Duration
	rateLimiter *time.Ticker
}

// NewHTTPIntegrationConnector creates a new HTTP-based integration connector
func NewHTTPIntegrationConnector(name, baseURL string) *HTTPIntegrationConnector {
	return &HTTPIntegrationConnector{
		name:      name,
		baseURL:   baseURL,
		headers:   make(map[string]string),
		authType:  "none",
		client:    &http.Client{},
		connected: false,
		timeout:   time.Second * 30,
	}
}

// SetBasicAuth sets basic authentication for the connector
func (c *HTTPIntegrationConnector) SetBasicAuth(username, password string) {
	c.authType = "basic"
	c.username = username
	c.password = password
}

// SetBearerAuth sets bearer token authentication for the connector
func (c *HTTPIntegrationConnector) SetBearerAuth(token string) {
	c.authType = "bearer"
	c.authToken = token
}

// SetHeader sets a custom header for requests
func (c *HTTPIntegrationConnector) SetHeader(key, value string) {
	c.headers[key] = value
}

// SetDefaultHeader is an alias for SetHeader for backward compatibility
func (c *HTTPIntegrationConnector) SetDefaultHeader(key, value string) {
	c.SetHeader(key, value)
}

// SetTimeout sets the request timeout
func (c *HTTPIntegrationConnector) SetTimeout(timeout time.Duration) {
	c.timeout = timeout
	c.client.Timeout = timeout
}

// SetRateLimit sets a rate limit for requests
func (c *HTTPIntegrationConnector) SetRateLimit(requestsPerMinute int) {
	var interval time.Duration
	if requestsPerMinute > 0 {
		interval = time.Minute / time.Duration(requestsPerMinute)
	} else {
		interval = time.Second // Default fallback
	}
	c.rateLimiter = time.NewTicker(interval)
}

// GetName returns the connector name
func (c *HTTPIntegrationConnector) GetName() string {
	return c.name
}

// Connect establishes a connection to the external service
func (c *HTTPIntegrationConnector) Connect(ctx context.Context) error {
	// For HTTP connectors, this could involve validation of the connection
	// by making a test request or just setting up the client
	c.client.Timeout = c.timeout

	// Set default headers
	if _, exists := c.headers["Content-Type"]; !exists {
		c.headers["Content-Type"] = "application/json"
	}

	c.connected = true
	return nil
}

// IsConnected checks if the connector is connected
func (c *HTTPIntegrationConnector) IsConnected() bool {
	return c.connected
}

// Disconnect closes the connection to the external service
func (c *HTTPIntegrationConnector) Disconnect(ctx context.Context) error {
	c.connected = false
	if c.rateLimiter != nil {
		c.rateLimiter.Stop()
		c.rateLimiter = nil
	}
	return nil
}

// Execute performs an action on the external service
func (c *HTTPIntegrationConnector) Execute(ctx context.Context, action string, params map[string]interface{}) (map[string]interface{}, error) {
	if !c.connected {
		return nil, fmt.Errorf("connector not connected")
	}

	// Rate limiting if enabled
	if c.rateLimiter != nil {
		select {
		case <-c.rateLimiter.C:
			// Rate limit satisfied, proceed
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	// Parse action into method and path
	parts := strings.SplitN(action, " ", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid action format: %s (expected 'METHOD /path')", action)
	}

	method := parts[0]
	path := parts[1]

	// Build URL
	reqURL, err := url.JoinPath(c.baseURL, path)
	if err != nil {
		return nil, fmt.Errorf("invalid URL path: %w", err)
	}

	// Handle query parameters for GET requests
	if method == "GET" && len(params) > 0 {
		queryParams := url.Values{}
		for k, v := range params {
			queryParams.Add(k, fmt.Sprintf("%v", v))
		}
		reqURL = reqURL + "?" + queryParams.Encode()
	}

	// Prepare request body for non-GET requests
	var body io.Reader
	if method != "GET" && params != nil {
		jsonData, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		body = strings.NewReader(string(jsonData))
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, method, reqURL, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add headers
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}

	// Add authentication
	switch c.authType {
	case "basic":
		req.SetBasicAuth(c.username, c.password)
	case "bearer":
		req.Header.Set("Authorization", "Bearer "+c.authToken)
	}

	// Execute request
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Parse response as JSON
	var result map[string]interface{}
	if len(respBody) > 0 {
		if err := json.Unmarshal(respBody, &result); err != nil {
			// If not JSON, return the raw response
			return map[string]interface{}{
				"statusCode": resp.StatusCode,
				"raw":        string(respBody),
			}, nil
		}
	} else {
		result = make(map[string]interface{})
	}

	// Add status code to result
	result["statusCode"] = resp.StatusCode

	// Check for error status codes
	if resp.StatusCode >= 400 {
		return result, fmt.Errorf("request returned error status: %d", resp.StatusCode)
	}

	return result, nil
}

// WebhookIntegrationConnector implements a connector that receives webhook callbacks
type WebhookIntegrationConnector struct {
	name      string
	path      string
	port      int
	server    *http.Server
	handlers  map[string]func(context.Context, map[string]interface{}) error
	connected bool
}

// NewWebhookIntegrationConnector creates a new webhook integration connector
func NewWebhookIntegrationConnector(name, path string, port int) *WebhookIntegrationConnector {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	return &WebhookIntegrationConnector{
		name:     name,
		path:     path,
		port:     port,
		handlers: make(map[string]func(context.Context, map[string]interface{}) error),
	}
}

// GetName returns the connector name
func (c *WebhookIntegrationConnector) GetName() string {
	return c.name
}

// Connect establishes the webhook server
func (c *WebhookIntegrationConnector) Connect(ctx context.Context) error {
	mux := http.NewServeMux()

	// Register handler for the webhook path
	mux.HandleFunc(c.path, func(w http.ResponseWriter, r *http.Request) {
		// Only allow POST requests
		if r.Method != "POST" {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Parse the request body
		defer r.Body.Close()
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Error reading request body", http.StatusBadRequest)
			return
		}

		// Parse JSON
		var payload map[string]interface{}
		if err := json.Unmarshal(body, &payload); err != nil {
			http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
			return
		}

		// Extract event type from payload
		eventType, _ := payload["type"].(string)
		if eventType == "" {
			eventType = "default"
		}

		// Find handler for this event type
		handler, exists := c.handlers[eventType]
		if !exists {
			handler, exists = c.handlers["default"]
			if !exists {
				http.Error(w, "No handler for event type", http.StatusNotImplemented)
				return
			}
		}

		// Execute handler
		if err := handler(r.Context(), payload); err != nil {
			http.Error(w, fmt.Sprintf("Error processing webhook: %v", err), http.StatusInternalServerError)
			return
		}

		// Return success
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	// Create server
	c.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", c.port),
		Handler: mux,
	}

	// Start server in a goroutine
	go func() {
		if err := c.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Printf("Webhook server error: %v\n", err)
		}
	}()

	c.connected = true
	return nil
}

// Disconnect stops the webhook server
func (c *WebhookIntegrationConnector) Disconnect(ctx context.Context) error {
	if c.server != nil {
		err := c.server.Shutdown(ctx)
		c.connected = false
		return err
	}
	return nil
}

// IsConnected checks if the connector is connected
func (c *WebhookIntegrationConnector) IsConnected() bool {
	return c.connected
}

// Execute is a no-op for webhook connectors (they are passive)
func (c *WebhookIntegrationConnector) Execute(ctx context.Context, action string, params map[string]interface{}) (map[string]interface{}, error) {
	return map[string]interface{}{"status": "webhook connectors do not support active execution"}, nil
}

// RegisterEventHandler registers a handler for a specific event type
func (c *WebhookIntegrationConnector) RegisterEventHandler(eventType string, handler func(context.Context, map[string]interface{}) error) {
	c.handlers[eventType] = handler
}

type IntegrationRegistry interface {
	// Name returns the name of the registry
	Name() string
	// Init initializes the registry
	Init(app modular.Application) error
	// Start starts the registry
	Start() error
	// Stop stops the registry
	Stop() error
	// RegisterConnector registers a new integration connector
	RegisterConnector(connector IntegrationConnector)
	// GetConnector retrieves a connector by name
	GetConnector(name string) (IntegrationConnector, error)
	// ListConnectors lists all registered connectors
	ListConnectors() []string
}

// StdIntegrationRegistry manages available integration connectors
type StdIntegrationRegistry struct {
	name       string
	connectors map[string]IntegrationConnector
}

// NewIntegrationRegistry creates a new integration registry
func NewIntegrationRegistry(name string) *StdIntegrationRegistry {
	return &StdIntegrationRegistry{
		name:       name,
		connectors: make(map[string]IntegrationConnector),
	}
}

// Name returns the module name
func (r *StdIntegrationRegistry) Name() string {
	return r.name
}

// Init initializes the registry with service dependencies
func (r *StdIntegrationRegistry) Init(app modular.Application) error {
	return app.RegisterService(r.name, r)
}

// Start starts all registered connectors
func (r *StdIntegrationRegistry) Start() error {
	ctx := context.Background()

	for name, connector := range r.connectors {
		if err := connector.Connect(ctx); err != nil {
			return fmt.Errorf("failed to start connector '%s': %w", name, err)
		}
	}

	return nil
}

// Stop stops all registered connectors
func (r *StdIntegrationRegistry) Stop() error {
	ctx := context.Background()

	for name, connector := range r.connectors {
		if err := connector.Disconnect(ctx); err != nil {
			return fmt.Errorf("failed to stop connector '%s': %w", name, err)
		}
	}

	return nil
}

// RegisterConnector adds a connector to the registry
func (r *StdIntegrationRegistry) RegisterConnector(connector IntegrationConnector) {
	r.connectors[connector.GetName()] = connector
}

// GetConnector retrieves a connector by name
func (r *StdIntegrationRegistry) GetConnector(name string) (IntegrationConnector, error) {
	connector, exists := r.connectors[name]
	if !exists {
		return nil, fmt.Errorf("connector '%s' not found", name)
	}
	return connector, nil
}

// ListConnectors returns all registered connectors
func (r *StdIntegrationRegistry) ListConnectors() []string {
	names := make([]string, 0, len(r.connectors))
	for name := range r.connectors {
		names = append(names, name)
	}
	return names
}
