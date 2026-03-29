package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

// newTestServer returns a minimal MCP server for scaffold tool testing.
func newTestServer() *Server {
	return NewServer("")
}

func TestHandleScaffoldCI_Basic(t *testing.T) {
	s := newTestServer()
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"description": "Go API server with Postgres",
		"binary_path": "./cmd/server",
	}

	result, err := s.handleScaffoldCI(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("tool returned error: %v", result.Content)
	}
	text := resultText(t, result)
	if !strings.Contains(text, "ci:") {
		t.Error("expected ci: section")
	}
	if !strings.Contains(text, "build:") {
		t.Error("expected build: section")
	}
	if !strings.Contains(text, "test:") {
		t.Error("expected test: section")
	}
	if !strings.Contains(text, "deploy:") {
		t.Error("expected deploy: section")
	}
	if !strings.Contains(text, "./cmd/server") {
		t.Error("expected binary path ./cmd/server")
	}
}

func TestHandleScaffoldCI_WithFrontend(t *testing.T) {
	s := newTestServer()
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"description": "React frontend with Go backend and Docker",
	}

	result, err := s.handleScaffoldCI(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := resultText(t, result)
	if !strings.Contains(text, "assets:") {
		t.Error("expected assets: section for frontend app")
	}
	if !strings.Contains(text, "containers:") {
		t.Error("expected containers: section for Docker app")
	}
}

func TestHandleScaffoldCI_MissingDescription(t *testing.T) {
	s := newTestServer()
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{}

	result, err := s.handleScaffoldCI(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error result for missing description")
	}
}

func TestHandleScaffoldEnvironment_Basic(t *testing.T) {
	s := newTestServer()
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"provider": "kubernetes",
	}

	result, err := s.handleScaffoldEnvironment(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := resultText(t, result)
	if !strings.Contains(text, "environments:") {
		t.Error("expected environments: section")
	}
	if !strings.Contains(text, "local:") {
		t.Error("expected local environment")
	}
	if !strings.Contains(text, "production:") {
		t.Error("expected production environment")
	}
}

func TestHandleScaffoldEnvironment_WithTailscale(t *testing.T) {
	s := newTestServer()
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"provider": "docker",
		"exposure": "tailscale",
	}

	result, err := s.handleScaffoldEnvironment(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := resultText(t, result)
	if !strings.Contains(text, "tailscale") {
		t.Error("expected tailscale exposure config")
	}
}

func TestHandleDetectSecrets_WithSecretFields(t *testing.T) {
	s := newTestServer()
	yamlContent := `
modules:
  - name: db
    type: database.postgres
    config:
      dsn: "${DATABASE_URL}"
  - name: auth
    type: auth.jwt
    config:
      signingKey: "${JWT_SECRET}"
`
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"yaml_content": yamlContent,
	}

	result, err := s.handleDetectSecrets(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("tool returned error: %v", result.Content)
	}
	text := resultText(t, result)
	if !strings.Contains(text, "candidates") {
		t.Error("expected candidates in result")
	}
	if !strings.Contains(text, "dsn") && !strings.Contains(text, "signingKey") {
		t.Error("expected secret field names in result")
	}
}

func TestHandleDetectPorts_Basic(t *testing.T) {
	s := newTestServer()
	yamlContent := `
modules:
  - name: server
    type: http.server
    config:
      port: 8080
`
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"yaml_content": yamlContent,
	}

	result, err := s.handleDetectPorts(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("tool returned error: %v", result.Content)
	}
	text := resultText(t, result)
	if !strings.Contains(text, "ports") {
		t.Error("expected ports in result")
	}
	if !strings.Contains(text, "8080") {
		t.Error("expected port 8080 to be detected")
	}
}

func TestHandleGenerateBootstrap_GitHubActions(t *testing.T) {
	s := newTestServer()
	yamlContent := `
ci:
  deploy:
    environments:
      staging:
        provider: kubernetes
      production:
        provider: kubernetes
        requireApproval: true
`
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"yaml_content": yamlContent,
		"platform":     "github-actions",
	}

	result, err := s.handleGenerateBootstrap(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := resultText(t, result)
	if !strings.Contains(text, "wfctl ci run") {
		t.Error("expected wfctl ci run call")
	}
	if !strings.Contains(text, "actions/checkout@v4") {
		t.Error("expected checkout action")
	}
}

func TestHandleGenerateBootstrap_GitLabCI(t *testing.T) {
	s := newTestServer()
	yamlContent := `
ci:
  deploy:
    environments:
      staging:
        provider: kubernetes
`
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"yaml_content": yamlContent,
		"platform":     "gitlab-ci",
	}

	result, err := s.handleGenerateBootstrap(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := resultText(t, result)
	if !strings.Contains(text, "stages:") {
		t.Error("expected stages: in gitlab-ci output")
	}
	if !strings.Contains(text, "wfctl ci run") {
		t.Error("expected wfctl ci run call")
	}
}

func TestHandleGenerateBootstrap_InvalidPlatform(t *testing.T) {
	s := newTestServer()
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"yaml_content": "modules: []",
		"platform":     "jenkins",
	}

	result, err := s.handleGenerateBootstrap(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for unsupported platform")
	}
}

func TestHandleDetectInfraNeeds_WithDB(t *testing.T) {
	s := newTestServer()
	yamlContent := `
modules:
  - name: db
    type: database.postgres
    config:
      dsn: "${DATABASE_URL}"
  - name: cache
    type: cache.redis
    config:
      addr: "localhost:6379"
`
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"yaml_content": yamlContent,
		"provider":     "aws",
	}

	result, err := s.handleDetectInfraNeeds(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("tool returned error: %v", result.Content)
	}
	text := resultText(t, result)
	if !strings.Contains(text, "aws-rds-postgres") {
		t.Error("expected aws-rds-postgres for database.postgres module")
	}
	if !strings.Contains(text, "aws-elasticache-redis") {
		t.Error("expected aws-elasticache-redis for cache.redis module")
	}
}

func TestHandleScaffoldInfra_NoModules(t *testing.T) {
	s := newTestServer()
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"yaml_content": "modules: []\n",
		"provider":     "aws",
	}

	result, err := s.handleScaffoldInfra(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := resultText(t, result)
	if !strings.Contains(text, "No infrastructure requirements") {
		t.Error("expected no-requirements message for empty modules")
	}
}

func TestSetupGuide_Content(t *testing.T) {
	s := newTestServer()
	req := mcp.ReadResourceRequest{}
	req.Params.URI = "workflow://docs/setup-guide"

	contents, err := s.handleSetupGuide(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(contents) == 0 {
		t.Fatal("expected non-empty contents")
	}
	text, ok := contents[0].(mcp.TextResourceContents)
	if !ok {
		t.Fatal("expected TextResourceContents")
	}
	if !strings.Contains(text.Text, "Workflow Setup Guide") {
		t.Error("expected setup guide title")
	}
	if !strings.Contains(text.Text, "scaffold_ci") {
		t.Error("expected scaffold_ci tool reference")
	}
}

// resultText extracts the text from the first content item of a CallToolResult.
func resultText(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	if len(result.Content) == 0 {
		t.Fatal("result has no content")
	}
	if text, ok := result.Content[0].(mcp.TextContent); ok {
		return text.Text
	}
	t.Fatalf("unexpected content type: %T", result.Content[0])
	return ""
}
