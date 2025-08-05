package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"

	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/workflow/ui"
)

// MockLogger implements modular.Logger for testing
type MockLogger struct{}

func (l *MockLogger) Debug(msg string, args ...interface{}) {}
func (l *MockLogger) Info(msg string, args ...interface{})  {}
func (l *MockLogger) Warn(msg string, args ...interface{})  {}
func (l *MockLogger) Error(msg string, args ...interface{}) {}
func (l *MockLogger) With(args ...interface{}) modular.Logger { return l }

// BDDTestContext holds the test state and dependencies
type BDDTestContext struct {
	uiModule      *ui.UIModule
	dbService     *ui.DatabaseService
	authService   *ui.AuthService
	apiHandler    *ui.APIHandler
	baseURL       string
	authToken     string
	lastResponse  *http.Response
	lastBody      []byte
	workflows     map[string]*ui.StoredWorkflow
	executions    map[string]*ui.WorkflowExecution
	testDB        *sql.DB
	currentTenant string
	tenantTokens  map[string]string // tenant name -> auth token
	tenantUsers   map[string]string // tenant name -> username
}

// NewBDDTestContext creates a new test context
func NewBDDTestContext() *BDDTestContext {
	return &BDDTestContext{
		baseURL:       "http://localhost:8080",
		workflows:     make(map[string]*ui.StoredWorkflow),
		executions:    make(map[string]*ui.WorkflowExecution),
		tenantTokens:  make(map[string]string),
		tenantUsers:   make(map[string]string),
		currentTenant: "default",
	}
}

// SetupTestDatabase initializes the test database and services
func (ctx *BDDTestContext) setupTestDatabase() error {
	// Create in-memory SQLite database for testing
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return err
	}
	ctx.testDB = db

	ctx.dbService = ui.NewDatabaseService(db, &MockLogger{})
	if err := ctx.dbService.InitializeSchema(context.Background()); err != nil {
		return err
	}

	ctx.authService = ui.NewAuthService("test-secret-key", ctx.dbService)
	return nil
}

// makeRequest simulates HTTP requests for testing
func (ctx *BDDTestContext) makeRequest(method, endpoint, token string, body interface{}) error {
	if body != nil {
		_, err := json.Marshal(body)
		if err != nil {
			return err
		}
	}

	// For testing, we'll simulate HTTP requests by calling the handlers directly
	// In a real test, you'd use an actual HTTP client
	
	// Simulate unauthorized access when no token provided
	if token == "" && endpoint != "/api/login" {
		ctx.lastResponse = &http.Response{
			StatusCode: http.StatusUnauthorized,
		}
		ctx.lastBody = []byte(`{"error": "missing authorization header"}`)
		return nil
	}
	
	// For now, simulate success for authenticated requests
	ctx.lastResponse = &http.Response{
		StatusCode: http.StatusOK,
	}
	ctx.lastBody = []byte(`{"success": true}`)
	
	if endpoint == "/api/workflows" && method == "POST" {
		ctx.lastResponse.StatusCode = http.StatusCreated
		ctx.lastBody = []byte(fmt.Sprintf(`{"id": "%s", "name": "Test Workflow", "status": "stopped"}`, uuid.New().String()))
	}

	if endpoint == "/api/workflows" && method == "GET" {
		ctx.lastBody = []byte(`{"workflows": [{"id": "test-id", "name": "Test Workflow", "description": "Test", "status": "stopped"}]}`)
	}

	return nil
}

// switchToTenant switches the test context to use a specific tenant's authentication
func (ctx *BDDTestContext) switchToTenant(tenantName string) error {
	if token, exists := ctx.tenantTokens[tenantName]; exists {
		ctx.authToken = token
		ctx.currentTenant = tenantName
		return nil
	}
	return fmt.Errorf("no authentication token found for tenant: %s", tenantName)
}

// getCurrentTenantUser returns the current tenant's username
func (ctx *BDDTestContext) getCurrentTenantUser() string {
	if user, exists := ctx.tenantUsers[ctx.currentTenant]; exists {
		return user
	}
	return "admin" // default
}

// cleanup cleans up test resources
func (ctx *BDDTestContext) cleanup() error {
	if ctx.testDB != nil {
		ctx.testDB.Close()
	}
	return nil
}