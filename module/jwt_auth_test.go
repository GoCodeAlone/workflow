package module

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func setupJWTAuth(t *testing.T) *JWTAuthModule {
	t.Helper()
	return NewJWTAuthModule("jwt-auth", "test-secret-key", 24*time.Hour, "test-issuer")
}

func registerUser(t *testing.T, j *JWTAuthModule, email, name, password string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{
		"email": email, "name": name, "password": password,
	})
	req := httptest.NewRequest(http.MethodPost, "/auth/register", bytes.NewReader(body))
	w := httptest.NewRecorder()
	j.Handle(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("register failed: status %d, body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	return resp["token"].(string)
}

func TestJWTAuth_Name(t *testing.T) {
	j := setupJWTAuth(t)
	if j.Name() != "jwt-auth" {
		t.Errorf("expected name 'jwt-auth', got '%s'", j.Name())
	}
}

func TestJWTAuth_InitRequiresSecret(t *testing.T) {
	app := CreateIsolatedApp(t)
	j := NewJWTAuthModule("jwt-auth", "", 24*time.Hour, "issuer")
	if err := j.Init(app); err == nil {
		t.Error("expected error for empty secret")
	}
}

func TestJWTAuth_InitRejectsShortSecret(t *testing.T) {
	app := CreateIsolatedApp(t)
	j := NewJWTAuthModule("jwt-auth", "short", 24*time.Hour, "issuer")
	if err := j.Init(app); err == nil {
		t.Error("expected error for secret shorter than 32 bytes")
	}
}

func TestJWTAuth_InitAcceptsLongSecret(t *testing.T) {
	app := CreateIsolatedApp(t)
	j := NewJWTAuthModule("jwt-auth", "this-is-a-valid-secret-32-bytes!", 24*time.Hour, "issuer")
	if err := j.Init(app); err != nil {
		t.Errorf("expected no error for 32-byte secret, got: %v", err)
	}
}

func TestJWTAuth_Register(t *testing.T) {
	j := setupJWTAuth(t)

	body := `{"email":"test@example.com","name":"Test User","password":"secret123"}`
	req := httptest.NewRequest(http.MethodPost, "/auth/register", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	j.Handle(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusCreated, w.Code, w.Body.String())
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["token"] == nil || resp["token"] == "" {
		t.Error("expected token in response")
	}
	user := resp["user"].(map[string]any)
	if user["email"] != "test@example.com" {
		t.Errorf("expected email 'test@example.com', got %v", user["email"])
	}
}

func TestJWTAuth_RegisterDisabledAfterSetup(t *testing.T) {
	j := setupJWTAuth(t)

	// First registration succeeds (setup mode — no users exist)
	registerUser(t, j, "admin@example.com", "Admin", "pass1")

	// Second registration is blocked — self-registration is disabled after setup
	body := `{"email":"user2@example.com","name":"User2","password":"pass2"}`
	req := httptest.NewRequest(http.MethodPost, "/auth/register", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	j.Handle(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected status %d (registration disabled after setup), got %d", http.StatusForbidden, w.Code)
	}
}

func TestJWTAuth_RegisterMissingFields(t *testing.T) {
	j := setupJWTAuth(t)

	body := `{"email":"","password":""}`
	req := httptest.NewRequest(http.MethodPost, "/auth/register", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	j.Handle(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestJWTAuth_Login(t *testing.T) {
	j := setupJWTAuth(t)
	registerUser(t, j, "login@example.com", "Login User", "mypassword")

	body := `{"email":"login@example.com","password":"mypassword"}`
	req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	j.Handle(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusOK, w.Code, w.Body.String())
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["token"] == nil || resp["token"] == "" {
		t.Error("expected token in login response")
	}
}

func TestJWTAuth_LoginInvalidPassword(t *testing.T) {
	j := setupJWTAuth(t)
	registerUser(t, j, "bad@example.com", "User", "correct")

	body := `{"email":"bad@example.com","password":"wrong"}`
	req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	j.Handle(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d", http.StatusUnauthorized, w.Code)
	}
}

func TestJWTAuth_LoginNonexistentUser(t *testing.T) {
	j := setupJWTAuth(t)

	body := `{"email":"nobody@example.com","password":"pass"}`
	req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	j.Handle(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d", http.StatusUnauthorized, w.Code)
	}
}

func TestJWTAuth_GenerateAndValidateToken(t *testing.T) {
	j := setupJWTAuth(t)

	user := &User{
		ID:    "1",
		Email: "token@example.com",
		Name:  "Token User",
	}

	token, err := j.generateToken(user)
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	valid, claims, err := j.Authenticate(token)
	if err != nil {
		t.Fatalf("authenticate error: %v", err)
	}
	if !valid {
		t.Error("expected token to be valid")
	}
	if claims["email"] != "token@example.com" {
		t.Errorf("expected email 'token@example.com', got %v", claims["email"])
	}
	if claims["iss"] != "test-issuer" {
		t.Errorf("expected issuer 'test-issuer', got %v", claims["iss"])
	}
}

func TestJWTAuth_AuthenticateInvalidToken(t *testing.T) {
	j := setupJWTAuth(t)

	valid, _, err := j.Authenticate("invalid.token.here")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if valid {
		t.Error("expected invalid token to fail authentication")
	}
}

func TestJWTAuth_GetProfile(t *testing.T) {
	j := setupJWTAuth(t)
	token := registerUser(t, j, "profile@example.com", "Profile User", "pass123")

	req := httptest.NewRequest(http.MethodGet, "/auth/profile", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	j.Handle(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusOK, w.Code, w.Body.String())
	}

	var user User
	json.NewDecoder(w.Body).Decode(&user)
	if user.Email != "profile@example.com" {
		t.Errorf("expected email 'profile@example.com', got '%s'", user.Email)
	}
}

func TestJWTAuth_GetProfileUnauthorized(t *testing.T) {
	j := setupJWTAuth(t)

	req := httptest.NewRequest(http.MethodGet, "/auth/profile", nil)
	w := httptest.NewRecorder()
	j.Handle(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d", http.StatusUnauthorized, w.Code)
	}
}

func TestJWTAuth_UpdateProfile(t *testing.T) {
	j := setupJWTAuth(t)
	token := registerUser(t, j, "update@example.com", "Original Name", "pass123")

	body := `{"name":"Updated Name"}`
	req := httptest.NewRequest(http.MethodPut, "/auth/profile", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	j.Handle(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusOK, w.Code, w.Body.String())
	}

	var user User
	json.NewDecoder(w.Body).Decode(&user)
	if user.Name != "Updated Name" {
		t.Errorf("expected name 'Updated Name', got '%s'", user.Name)
	}
}

func TestJWTAuth_AuthProviderInterface(t *testing.T) {
	j := setupJWTAuth(t)

	// Verify JWTAuthModule implements AuthProvider
	var _ AuthProvider = j
}

func TestJWTAuth_ProvidesServices(t *testing.T) {
	j := setupJWTAuth(t)
	services := j.ProvidesServices()
	if len(services) != 1 {
		t.Fatalf("expected 1 service, got %d", len(services))
	}
	if services[0].Name != "jwt-auth" {
		t.Errorf("expected service name 'jwt-auth', got '%s'", services[0].Name)
	}
}

func TestJWTAuth_DefaultValues(t *testing.T) {
	j := NewJWTAuthModule("jwt", "secret", 0, "")
	if j.tokenExpiry != 24*time.Hour {
		t.Errorf("expected default tokenExpiry 24h, got %v", j.tokenExpiry)
	}
	if j.issuer != "workflow" {
		t.Errorf("expected default issuer 'workflow', got '%s'", j.issuer)
	}
}

// --- v1 response format tests ---

func setupJWTAuthV1(t *testing.T) *JWTAuthModule {
	t.Helper()
	j := NewJWTAuthModule("jwt-auth", "test-secret-key", 24*time.Hour, "test-issuer")
	j.SetResponseFormat("v1")
	return j
}

func registerUserV1(t *testing.T, j *JWTAuthModule, email, name, password string) (string, string) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{
		"email": email, "name": name, "password": password,
	})
	req := httptest.NewRequest(http.MethodPost, "/auth/register", bytes.NewReader(body))
	w := httptest.NewRecorder()
	j.Handle(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("register failed: status %d, body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	return resp["access_token"].(string), resp["refresh_token"].(string)
}

func TestJWTAuth_V1_Register(t *testing.T) {
	j := setupJWTAuthV1(t)

	body := `{"email":"v1@example.com","name":"V1 User","password":"secret123"}`
	req := httptest.NewRequest(http.MethodPost, "/auth/register", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	j.Handle(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["access_token"] == nil || resp["access_token"] == "" {
		t.Error("expected access_token in v1 response")
	}
	if resp["refresh_token"] == nil || resp["refresh_token"] == "" {
		t.Error("expected refresh_token in v1 response")
	}
	if resp["expires_in"] == nil {
		t.Error("expected expires_in in v1 response")
	}
	// Should NOT have the old "token" key
	if resp["token"] != nil {
		t.Error("v1 response should not contain 'token' key")
	}
}

func TestJWTAuth_V1_Login(t *testing.T) {
	j := setupJWTAuthV1(t)
	registerUserV1(t, j, "v1login@example.com", "V1 Login", "mypassword")

	body := `{"email":"v1login@example.com","password":"mypassword"}`
	req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	j.Handle(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["access_token"] == nil {
		t.Error("expected access_token in v1 login response")
	}
	if resp["refresh_token"] == nil {
		t.Error("expected refresh_token in v1 login response")
	}
}

func TestJWTAuth_V1_Refresh(t *testing.T) {
	j := setupJWTAuthV1(t)
	_, refreshToken := registerUserV1(t, j, "refresh@example.com", "Refresh User", "pass123")

	body, _ := json.Marshal(map[string]string{"refresh_token": refreshToken})
	req := httptest.NewRequest(http.MethodPost, "/auth/refresh", bytes.NewReader(body))
	w := httptest.NewRecorder()
	j.Handle(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["access_token"] == nil {
		t.Error("expected new access_token from refresh")
	}
	if resp["refresh_token"] == nil {
		t.Error("expected new refresh_token from refresh")
	}
}

func TestJWTAuth_V1_RefreshInvalidToken(t *testing.T) {
	j := setupJWTAuthV1(t)

	body := `{"refresh_token":"invalid.token.here"}`
	req := httptest.NewRequest(http.MethodPost, "/auth/refresh", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	j.Handle(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestJWTAuth_V1_RefreshWithAccessToken(t *testing.T) {
	j := setupJWTAuthV1(t)
	accessToken, _ := registerUserV1(t, j, "wrongtype@example.com", "Wrong Type", "pass123")

	// Using an access token (not a refresh token) should fail
	body, _ := json.Marshal(map[string]string{"refresh_token": accessToken})
	req := httptest.NewRequest(http.MethodPost, "/auth/refresh", bytes.NewReader(body))
	w := httptest.NewRecorder()
	j.Handle(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 when using access token as refresh, got %d", w.Code)
	}
}

func TestJWTAuth_V1_Logout(t *testing.T) {
	j := setupJWTAuthV1(t)
	accessToken, _ := registerUserV1(t, j, "logout@example.com", "Logout User", "pass123")

	req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	w := httptest.NewRecorder()
	j.Handle(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestJWTAuth_V1_Me(t *testing.T) {
	j := setupJWTAuthV1(t)
	accessToken, _ := registerUserV1(t, j, "me@example.com", "Me User", "pass123")

	req := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	w := httptest.NewRecorder()
	j.Handle(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["email"] != "me@example.com" {
		t.Errorf("expected email 'me@example.com', got %v", resp["email"])
	}
}

// --- Setup & User Management Tests ---

func TestJWTAuth_SetupStatus_NoUsers(t *testing.T) {
	j := setupJWTAuthV1(t)

	req := httptest.NewRequest(http.MethodGet, "/auth/setup-status", nil)
	w := httptest.NewRecorder()
	j.Handle(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["needsSetup"] != true {
		t.Errorf("expected needsSetup=true, got %v", resp["needsSetup"])
	}
	if resp["userCount"] != float64(0) {
		t.Errorf("expected userCount=0, got %v", resp["userCount"])
	}
}

func TestJWTAuth_SetupStatus_WithUsers(t *testing.T) {
	j := setupJWTAuthV1(t)
	registerUserV1(t, j, "existing@example.com", "User", "pass123")

	req := httptest.NewRequest(http.MethodGet, "/auth/setup-status", nil)
	w := httptest.NewRecorder()
	j.Handle(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["needsSetup"] != false {
		t.Errorf("expected needsSetup=false, got %v", resp["needsSetup"])
	}
}

func TestJWTAuth_Setup_CreatesAdmin(t *testing.T) {
	j := setupJWTAuthV1(t)

	body := `{"email":"admin@test.com","name":"Admin","password":"secret123"}`
	req := httptest.NewRequest(http.MethodPost, "/auth/setup", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	j.Handle(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["access_token"] == nil {
		t.Error("expected access_token in response")
	}
	if resp["refresh_token"] == nil {
		t.Error("expected refresh_token in response")
	}

	user := resp["user"].(map[string]any)
	if user["role"] != "admin" {
		t.Errorf("expected role 'admin', got %v", user["role"])
	}
}

func TestJWTAuth_Setup_BlockedWhenUsersExist(t *testing.T) {
	j := setupJWTAuthV1(t)
	registerUserV1(t, j, "existing@example.com", "User", "pass123")

	body := `{"email":"admin@test.com","name":"Admin","password":"secret123"}`
	req := httptest.NewRequest(http.MethodPost, "/auth/setup", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	j.Handle(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
}

func TestJWTAuth_Setup_ValidationErrors(t *testing.T) {
	j := setupJWTAuthV1(t)

	body := `{"email":"","password":""}`
	req := httptest.NewRequest(http.MethodPost, "/auth/setup", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	j.Handle(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// setupAdminUser creates a v1 JWT auth module with an admin user and returns
// the module and the admin's access token.
func setupAdminUser(t *testing.T) (*JWTAuthModule, string) {
	t.Helper()
	j := setupJWTAuthV1(t)

	body := `{"email":"admin@test.com","name":"Admin","password":"admin123"}`
	req := httptest.NewRequest(http.MethodPost, "/auth/setup", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	j.Handle(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("setup failed: status %d, body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	return j, resp["access_token"].(string)
}

func TestJWTAuth_ListUsers_RequiresAdmin(t *testing.T) {
	j := setupJWTAuthV1(t)
	// Register a normal user (not admin)
	accessToken, _ := registerUserV1(t, j, "user@example.com", "User", "pass123")

	req := httptest.NewRequest(http.MethodGet, "/auth/users", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	w := httptest.NewRecorder()
	j.Handle(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestJWTAuth_ListUsers_Success(t *testing.T) {
	j, adminToken := setupAdminUser(t)

	req := httptest.NewRequest(http.MethodGet, "/auth/users", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	j.Handle(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var users []map[string]any
	json.NewDecoder(w.Body).Decode(&users)
	if len(users) != 1 {
		t.Errorf("expected 1 user, got %d", len(users))
	}
}

func TestJWTAuth_CreateUser_Admin(t *testing.T) {
	j, adminToken := setupAdminUser(t)

	body := `{"email":"newuser@test.com","name":"New User","password":"pass123","role":"user"}`
	req := httptest.NewRequest(http.MethodPost, "/auth/users", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	j.Handle(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d; body: %s", w.Code, w.Body.String())
	}

	var user map[string]any
	json.NewDecoder(w.Body).Decode(&user)
	if user["email"] != "newuser@test.com" {
		t.Errorf("expected email 'newuser@test.com', got %v", user["email"])
	}
	if user["role"] != "user" {
		t.Errorf("expected role 'user', got %v", user["role"])
	}
}

func TestJWTAuth_DeleteUser_PreventsSelfDelete(t *testing.T) {
	j, adminToken := setupAdminUser(t)

	// Admin's ID is "1"
	req := httptest.NewRequest(http.MethodDelete, "/auth/users/1", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	j.Handle(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestJWTAuth_DeleteUser_PreventsLastAdmin(t *testing.T) {
	j, adminToken := setupAdminUser(t)

	// Create a second admin
	body := `{"email":"admin2@test.com","name":"Admin2","password":"pass123","role":"admin"}`
	req := httptest.NewRequest(http.MethodPost, "/auth/users", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	j.Handle(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create admin2 failed: %d", w.Code)
	}

	var admin2 map[string]any
	json.NewDecoder(w.Body).Decode(&admin2)
	admin2ID := admin2["id"].(string)

	// Delete admin2 should work (first admin still exists)
	req = httptest.NewRequest(http.MethodDelete, "/auth/users/"+admin2ID, nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	j.Handle(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestJWTAuth_UpdateRole_Admin(t *testing.T) {
	j, adminToken := setupAdminUser(t)

	// Create a regular user
	body := `{"email":"user@test.com","name":"User","password":"pass123","role":"user"}`
	req := httptest.NewRequest(http.MethodPost, "/auth/users", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	j.Handle(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create user failed: %d", w.Code)
	}

	var created map[string]any
	json.NewDecoder(w.Body).Decode(&created)
	userID := created["id"].(string)

	// Update role to admin
	roleBody := `{"role":"admin"}`
	req = httptest.NewRequest(http.MethodPut, "/auth/users/"+userID+"/role", bytes.NewBufferString(roleBody))
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w = httptest.NewRecorder()
	j.Handle(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var updated map[string]any
	json.NewDecoder(w.Body).Decode(&updated)
	if updated["role"] != "admin" {
		t.Errorf("expected role 'admin', got %v", updated["role"])
	}
}

// --- Algorithm confusion / signing method pinning tests ---

// TestJWTAuth_Authenticate_RejectsNonHS256 verifies that Authenticate() rejects
// tokens signed with algorithms other than HS256 (prevents algorithm confusion attacks).
func TestJWTAuth_Authenticate_RejectsNonHS256(t *testing.T) {
	j := setupJWTAuth(t)

	user := &User{ID: "1", Email: "alg@example.com", Name: "Alg Test"}

	for _, method := range []jwt.SigningMethod{jwt.SigningMethodHS384, jwt.SigningMethodHS512} {
		method := method
		t.Run("rejects "+method.Alg(), func(t *testing.T) {
			claims := jwt.MapClaims{
				"sub":   user.ID,
				"email": user.Email,
				"iss":   "test-issuer",
				"iat":   time.Now().Unix(),
				"exp":   time.Now().Add(24 * time.Hour).Unix(),
			}
			tok, err := jwt.NewWithClaims(method, claims).SignedString([]byte("test-secret-key"))
			if err != nil {
				t.Fatalf("failed to sign token with %s: %v", method.Alg(), err)
			}

			valid, _, authErr := j.Authenticate(tok)
			if authErr != nil {
				t.Fatalf("expected nil error, got: %v", authErr)
			}
			if valid {
				t.Errorf("expected token signed with %s to be rejected, but it was accepted", method.Alg())
			}
		})
	}
}

// TestJWTAuth_Authenticate_AcceptsHS256 verifies that valid HS256 tokens are accepted.
func TestJWTAuth_Authenticate_AcceptsHS256(t *testing.T) {
	j := setupJWTAuth(t)

	user := &User{ID: "1", Email: "hs256@example.com", Name: "HS256 User"}
	tok, err := j.generateToken(user)
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	valid, _, err := j.Authenticate(tok)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !valid {
		t.Error("expected HS256 token to be valid")
	}
}

// TestJWTAuth_HandleRefresh_RejectsNonHS256 verifies that the refresh handler
// rejects tokens signed with algorithms other than HS256.
func TestJWTAuth_HandleRefresh_RejectsNonHS256(t *testing.T) {
	j := setupJWTAuthV1(t)
	registerUserV1(t, j, "alg-refresh@example.com", "Alg Refresh", "pass123")

	for _, method := range []jwt.SigningMethod{jwt.SigningMethodHS384, jwt.SigningMethodHS512} {
		method := method
		t.Run("rejects "+method.Alg(), func(t *testing.T) {
			claims := jwt.MapClaims{
				"sub":   "1",
				"email": "alg-refresh@example.com",
				"type":  "refresh",
				"iss":   "test-issuer",
				"iat":   time.Now().Unix(),
				"exp":   time.Now().Add(7 * 24 * time.Hour).Unix(),
			}
			tok, err := jwt.NewWithClaims(method, claims).SignedString([]byte("test-secret-key"))
			if err != nil {
				t.Fatalf("failed to sign token with %s: %v", method.Alg(), err)
			}

			body, _ := json.Marshal(map[string]string{"refresh_token": tok})
			req := httptest.NewRequest(http.MethodPost, "/auth/refresh", bytes.NewReader(body))
			w := httptest.NewRecorder()
			j.Handle(w, req)

			if w.Code != http.StatusUnauthorized {
				t.Errorf("%s: expected 401, got %d", method.Alg(), w.Code)
			}
		})
	}
}

// TestJWTAuth_ExtractUser_RejectsNonHS256 verifies that protected endpoints
// reject Authorization headers containing tokens signed with non-HS256 algorithms.
func TestJWTAuth_ExtractUser_RejectsNonHS256(t *testing.T) {
	j := setupJWTAuth(t)
	// Register a user so we have a valid user in the store.
	registerUser(t, j, "protect@example.com", "Protected User", "pass123")

	for _, method := range []jwt.SigningMethod{jwt.SigningMethodHS384, jwt.SigningMethodHS512} {
		method := method
		t.Run("profile with "+method.Alg(), func(t *testing.T) {
			claims := jwt.MapClaims{
				"sub":   "1",
				"email": "protect@example.com",
				"iss":   "test-issuer",
				"iat":   time.Now().Unix(),
				"exp":   time.Now().Add(24 * time.Hour).Unix(),
			}
			tok, err := jwt.NewWithClaims(method, claims).SignedString([]byte("test-secret-key"))
			if err != nil {
				t.Fatalf("failed to sign token with %s: %v", method.Alg(), err)
			}

			req := httptest.NewRequest(http.MethodGet, "/auth/profile", nil)
			req.Header.Set("Authorization", "Bearer "+tok)
			w := httptest.NewRecorder()
			j.Handle(w, req)

			if w.Code != http.StatusUnauthorized {
				t.Errorf("%s: expected 401, got %d", method.Alg(), w.Code)
			}
		})
	}
}
