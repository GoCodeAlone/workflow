package module

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
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
	var resp map[string]interface{}
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

func TestJWTAuth_Register(t *testing.T) {
	j := setupJWTAuth(t)

	body := `{"email":"test@example.com","name":"Test User","password":"secret123"}`
	req := httptest.NewRequest(http.MethodPost, "/auth/register", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	j.Handle(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusCreated, w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["token"] == nil || resp["token"] == "" {
		t.Error("expected token in response")
	}
	user := resp["user"].(map[string]interface{})
	if user["email"] != "test@example.com" {
		t.Errorf("expected email 'test@example.com', got %v", user["email"])
	}
}

func TestJWTAuth_RegisterDuplicateEmail(t *testing.T) {
	j := setupJWTAuth(t)

	registerUser(t, j, "dup@example.com", "User1", "pass1")

	body := `{"email":"dup@example.com","name":"User2","password":"pass2"}`
	req := httptest.NewRequest(http.MethodPost, "/auth/register", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	j.Handle(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("expected status %d for duplicate, got %d", http.StatusConflict, w.Code)
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

	var resp map[string]interface{}
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
