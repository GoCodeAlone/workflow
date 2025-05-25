package module

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestUIServer(t *testing.T) {
	// Create a UI server
	uiServer := NewUIServer("test-ui", ":8080", nil)

	// Start the server
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	err := uiServer.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start UI server: %v", err)
	}

	// Create a test request to get workflows
	req := httptest.NewRequest(http.MethodGet, "/api/workflows", nil)
	w := httptest.NewRecorder()

	// Serve the request
	uiServer.ServeHTTP(w, req)

	// Check the response
	if w.Code != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, w.Code)
	}

	var response map[string]interface{}
	err = json.Unmarshal(w.Body.Bytes(), &response)
	if err != nil {
		t.Fatalf("Failed to parse response body: %v", err)
	}

	workflows, ok := response["workflows"]
	if !ok {
		t.Errorf("Response missing 'workflows' field")
	}

	// Verify workflows is an array
	_, ok = workflows.([]interface{})
	if !ok {
		t.Errorf("Expected workflows to be an array")
	}

	// Stop the server
	err = uiServer.Stop(ctx)
	if err != nil {
		t.Fatalf("Failed to stop UI server: %v", err)
	}
}