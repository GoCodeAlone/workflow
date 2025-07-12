package module

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/GoCodeAlone/modular"
)

func TestSimpleHTTPHandler(t *testing.T) {
	// Create a new handler
	handler := NewSimpleHTTPHandler("test-handler", "application/json")

	// Create a request to pass to our handler
	req, err := http.NewRequest("GET", "/test", nil)
	if err != nil {
		t.Fatal(err)
	}

	// Create a ResponseRecorder to record the response
	recorder := httptest.NewRecorder()

	// Call the handler
	handler.Handle(recorder, req)

	// Check status code
	if recorder.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", recorder.Code)
	}

	// Check content type
	if contentType := recorder.Header().Get("Content-Type"); contentType != "application/json" {
		t.Errorf("expected Content-Type %s, got %s", "application/json", contentType)
	}

	// Check response body contains handler name
	var response map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Errorf("failed to parse response body: %v", err)
	}

	if handlerName, exists := response["handler"]; !exists || handlerName != "test-handler" {
		t.Errorf("expected handler name in response to be %s", "test-handler")
	}
}

func TestRESTAPIHandler(t *testing.T) {
	// Create a new API handler
	apiHandler := NewRESTAPIHandler("users-api", "users")

	// Test POST request - Create user
	userData := `{"name":"John Doe","email":"john@example.com"}`
	postReq, _ := http.NewRequest("POST", "/api/users", strings.NewReader(userData))
	postRecorder := httptest.NewRecorder()

	apiHandler.Handle(postRecorder, postReq)

	// Check status code for creation
	if postRecorder.Code != http.StatusCreated {
		t.Errorf("expected status 201 for POST, got %d", postRecorder.Code)
	}

	// Test GET request - List users
	getReq, _ := http.NewRequest("GET", "/api/users", nil)
	getRecorder := httptest.NewRecorder()

	apiHandler.Handle(getRecorder, getReq)

	// Check status code for listing
	if getRecorder.Code != http.StatusOK {
		t.Errorf("expected status 200 for GET, got %d", getRecorder.Code)
	}

	// Parse response to ensure it contains our created user
	var users []RESTResource
	if err := json.Unmarshal(getRecorder.Body.Bytes(), &users); err != nil {
		t.Errorf("failed to parse GET response: %v", err)
	}

	if len(users) != 1 {
		t.Errorf("expected 1 user, got %d", len(users))
	}
}

func TestHTTPRouter(t *testing.T) {
	// Create router and handlers
	router := NewStandardHTTPRouter("test-router")
	handler1 := NewSimpleHTTPHandler("test-handler-1", "application/json")
	handler2 := NewSimpleHTTPHandler("test-handler-2", "application/json")

	// Add routes
	router.AddRoute("GET", "/route1", handler1)
	router.AddRoute("POST", "/route2", handler2)

	// Test first route
	req1, _ := http.NewRequest("GET", "/route1", nil)
	rec1 := httptest.NewRecorder()

	if err := router.Start(context.Background()); err != nil {
		t.Fatalf("Failed to start router: %v", err)
	}
	router.ServeHTTP(rec1, req1)

	if rec1.Code != http.StatusOK {
		t.Errorf("route1: expected status 200, got %d", rec1.Code)
	}

	// Test second route
	req2, _ := http.NewRequest("POST", "/route2", nil)
	rec2 := httptest.NewRecorder()

	router.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Errorf("route2: expected status 200, got %d", rec2.Code)
	}

	// Test non-existent route
	req3, _ := http.NewRequest("GET", "/not-found", nil)
	rec3 := httptest.NewRecorder()

	router.ServeHTTP(rec3, req3)

	if rec3.Code != http.StatusNotFound {
		t.Errorf("non-existent route: expected status 404, got %d", rec3.Code)
	}
}

func TestAuthMiddleware(t *testing.T) {
	// Create middleware
	auth := NewAuthMiddleware("auth-middleware", "Bearer")

	// Create a test handler
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Get claims from context
		claims, ok := r.Context().Value(authClaimsContextKey).(map[string]interface{})
		if !ok {
			t.Error("auth claims not found in request context")
			return
		}

		username, ok := claims["username"].(string)
		if !ok {
			t.Error("username not found in auth claims")
			return
		}

		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte(username)); err != nil {
			// Log error but continue since response is already committed
			_ = err
		}
	})

	// Add valid tokens
	auth.AddProvider(map[string]map[string]interface{}{
		"test-token": {
			"username": "testuser",
			"role":     "admin",
		},
	})

	// Apply middleware
	handler := auth.Process(testHandler)

	// Test with valid token
	validReq, _ := http.NewRequest("GET", "/", nil)
	validReq.Header.Set("Authorization", "Bearer test-token")
	validRec := httptest.NewRecorder()

	handler.ServeHTTP(validRec, validReq)

	if validRec.Code != http.StatusOK {
		t.Errorf("valid token: expected status 200, got %d", validRec.Code)
	}

	// Check username
	if username := validRec.Body.String(); username != "testuser" {
		t.Errorf("expected username 'testuser', got '%s'", username)
	}

	// Test with invalid token
	invalidReq, _ := http.NewRequest("GET", "/", nil)
	invalidReq.Header.Set("Authorization", "Bearer invalid-token")
	invalidRec := httptest.NewRecorder()

	handler.ServeHTTP(invalidRec, invalidReq)

	if invalidRec.Code != http.StatusUnauthorized {
		t.Errorf("invalid token: expected status 401, got %d", invalidRec.Code)
	}
}



type minCfg struct {
	Modules []interface{}          `json:"modules"`
	Env     map[string]interface{} `json:"env"`
	App     struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"app"`
}

func TestHTTPModulesIntegration(t *testing.T) {
	// Only skip the test if it's a short run to avoid service conflicts in CI environments
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Generate unique names for this test run to avoid conflicts
	uniqueSuffix := fmt.Sprintf("%d", time.Now().UnixNano())
	serverName := fmt.Sprintf("http-server-%s", uniqueSuffix)
	routerName := fmt.Sprintf("test-router-%s", uniqueSuffix)
	handlerName := fmt.Sprintf("test-handler-%s", uniqueSuffix)
	authName := fmt.Sprintf("test-auth-%s", uniqueSuffix)

	// Use a mock logger to avoid nil pointer issues
	logger := &mockLogger{entries: make([]string, 0)}

	t.Setenv("APP_NAME", fmt.Sprintf("test-http-app-%s", uniqueSuffix))
	t.Setenv("APP_VERSION", "1.0.0")

	configProvider := modular.NewStdConfigProvider(&minCfg{})
	app := modular.NewStdApplication(configProvider, logger)

	// Create HTTP modules with unique names
	server := NewStandardHTTPServer(serverName, ":0") // Use port 0 for testing
	router := NewStandardHTTPRouter(routerName)

	// Register modules before initializing the app
	app.RegisterModule(server)
	app.RegisterModule(router)

	// Crucial step: Update router's server dependency to match our unique server name
	// Make sure the server name is exactly the same as the one created above
	router.SetServerDependencies([]string{serverName})

	handler := NewSimpleHTTPHandler(handlerName, "application/json")
	auth := NewAuthMiddleware(authName, "Bearer")

	// Register handler and auth modules
	app.RegisterModule(handler)
	app.RegisterModule(auth)

	// Now initialize the app with all modules already registered
	if err := app.Init(); err != nil {
		t.Fatalf("Failed to initialize app: %v", err)
	}

	// Start modules
	if err := app.Start(); err != nil {
		t.Fatalf("Failed to start modules: %v", err)
	}

	// Add router to server
	server.AddRouter(router)

	// Add route with middleware - using adapter to convert from http.Handler to HTTPHandler
	processedHandler := auth.Process(handler)
	router.AddRoute("GET", "/test", NewHTTPHandlerAdapter(processedHandler))

	// Clean up
	defer func() {
		if err := app.Stop(); err != nil {
			t.Errorf("Failed to stop app: %v", err)
		}
	}()
}

// mockLogger implements a simple logger for testing
type mockLogger struct {
	entries []string
}

func (l *mockLogger) Debug(message string, args ...interface{}) {
	l.entries = append(l.entries, fmt.Sprintf(message, args...))
}

func (l *mockLogger) Info(message string, args ...interface{}) {
	l.entries = append(l.entries, fmt.Sprintf(message, args...))
}

func (l *mockLogger) Warning(message string, args ...interface{}) {
	l.entries = append(l.entries, fmt.Sprintf(message, args...))
}

func (l *mockLogger) Warn(message string, args ...interface{}) {
	l.entries = append(l.entries, fmt.Sprintf(message, args...))
}

func (l *mockLogger) Error(message string, args ...interface{}) {
	l.entries = append(l.entries, fmt.Sprintf(message, args...))
}

func (l *mockLogger) Critical(message string, args ...interface{}) {
	l.entries = append(l.entries, fmt.Sprintf(message, args...))
}
