package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/GoCodeAlone/workflow/ui"
)

// Authentication-related step definitions

func (ctx *BDDTestContext) theWorkflowUIIsRunning() error {
	return ctx.setupTestDatabase()
}

func (ctx *BDDTestContext) thereIsADefaultTenant(tenantName string) error {
	// Database initialization already creates a default tenant
	return nil
}

func (ctx *BDDTestContext) thereIsAnAdminUser(username, password string) error {
	// Database initialization already creates an admin user
	return nil
}

func (ctx *BDDTestContext) thereIsATenantNamed(tenantName string) error {
	// For testing, we assume the tenant exists or create it
	ctx.tenantUsers[tenantName] = "admin"
	return nil
}

func (ctx *BDDTestContext) thereIsAUserInTenant(username, tenantName string) error {
	ctx.tenantUsers[tenantName] = username
	return nil
}

func (ctx *BDDTestContext) iLoginWithUsernameAndPassword(username, password string) error {
	loginReq := &ui.LoginRequest{
		Username: username,
		Password: password,
	}

	response, err := ctx.authService.Login(context.Background(), loginReq)
	if err != nil {
		// Store error for verification
		ctx.lastBody = []byte(err.Error())
		return nil
	}

	ctx.authToken = response.Token
	// Store token for current tenant
	ctx.tenantTokens[ctx.currentTenant] = response.Token
	responseBytes, _ := json.Marshal(response)
	ctx.lastBody = responseBytes
	return nil
}

func (ctx *BDDTestContext) iLoginAsTenant(username, tenantName string) error {
	// Switch to the specified tenant context
	ctx.currentTenant = tenantName
	err := ctx.iLoginWithUsernameAndPassword(username, "admin") // Use default password
	if err != nil {
		return err
	}
	// Store the token for this tenant
	ctx.tenantTokens[tenantName] = ctx.authToken
	return nil
}

func (ctx *BDDTestContext) iShouldReceiveAValidJWTToken() error {
	if ctx.authToken == "" {
		return fmt.Errorf("no token received")
	}

	// Validate token
	_, err := ctx.authService.ValidateToken(ctx.authToken)
	if err != nil {
		return fmt.Errorf("invalid token: %w", err)
	}

	return nil
}

func (ctx *BDDTestContext) iShouldSeeMyUserInformation() error {
	var response ui.LoginResponse
	if err := json.Unmarshal(ctx.lastBody, &response); err != nil {
		return err
	}

	if response.User.Username == "" {
		return fmt.Errorf("no user information in response")
	}

	return nil
}

func (ctx *BDDTestContext) iShouldSeeMyTenantInformation() error {
	var response ui.LoginResponse
	if err := json.Unmarshal(ctx.lastBody, &response); err != nil {
		return err
	}

	if response.Tenant.Name == "" {
		return fmt.Errorf("no tenant information in response")
	}

	return nil
}

func (ctx *BDDTestContext) iShouldReceiveAnAuthenticationError() error {
	if ctx.authToken != "" {
		return fmt.Errorf("unexpectedly received a token")
	}

	if !bytes.Contains(ctx.lastBody, []byte("authentication failed")) &&
		!bytes.Contains(ctx.lastBody, []byte("invalid credentials")) {
		return fmt.Errorf("expected authentication error, got: %s", ctx.lastBody)
	}

	return nil
}

func (ctx *BDDTestContext) iShouldNotReceiveAToken() error {
	if ctx.authToken != "" {
		return fmt.Errorf("unexpectedly received a token")
	}
	return nil
}

func (ctx *BDDTestContext) iTryToAccessWithoutAToken(endpoint string) error {
	return ctx.makeRequest("GET", endpoint, "", nil)
}

func (ctx *BDDTestContext) iShouldReceiveAnUnauthorizedError() error {
	if ctx.lastResponse == nil {
		return fmt.Errorf("no response received")
	}

	if ctx.lastResponse.StatusCode != http.StatusUnauthorized {
		return fmt.Errorf("expected 401 Unauthorized, got %d", ctx.lastResponse.StatusCode)
	}

	return nil
}

func (ctx *BDDTestContext) iAmLoggedInAs(username string) error {
	return ctx.iLoginWithUsernameAndPassword(username, "admin")
}

func (ctx *BDDTestContext) iAccessWithMyToken(endpoint string) error {
	return ctx.makeRequest("GET", endpoint, ctx.authToken, nil)
}

func (ctx *BDDTestContext) iShouldReceiveASuccessfulResponse() error {
	if ctx.lastResponse == nil {
		return fmt.Errorf("no response received")
	}

	if ctx.lastResponse.StatusCode < 200 || ctx.lastResponse.StatusCode >= 300 {
		return fmt.Errorf("expected successful response (2xx), got %d", ctx.lastResponse.StatusCode)
	}

	return nil
}