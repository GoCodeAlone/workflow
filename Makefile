.PHONY: build build-ui build-go test lint fmt vet fix install-hooks clean

# Full build: UI + Go binary (use this when admin UI has changed)
build: build-ui build-go

# Build the React admin UI and copy assets for go:embed
build-ui:
	@echo "Building admin UI..."
	cd ui && npm install --silent 2>/dev/null && npx vite build
	@rm -rf module/ui_dist/assets module/ui_dist/index.html module/ui_dist/vite.svg
	@cp -r ui/dist/* module/ui_dist/
	@echo "Admin UI assets copied to module/ui_dist/"

# Build Go binary only (assumes UI assets already in module/ui_dist/)
build-go:
	go build -o server ./cmd/server

# Build MCP server binary
build-mcp:
	go build -o workflow-mcp-server ./cmd/workflow-mcp-server

# Run all tests with race detection
test:
	go test -race ./...

# Run golangci-lint
lint:
	golangci-lint run --timeout=5m

# Format code
fmt:
	go fmt ./...

# Run go vet
vet:
	go vet ./...

# Run go fix modernizations (preview only)
fix-preview:
	go fix -diff ./...

# Apply go fix modernizations
fix:
	go fix ./...

# Install git hooks
install-hooks:
	./scripts/install-hooks.sh

# Build example binary
build-examples:
	cd example && go build -o workflow-example ./...

# Validate all example configs load without error
test-configs:
	go test -run TestExampleConfigsLoad -v ./...

# Run everything CI would run
ci: fmt vet test lint

# Run with admin UI enabled
run-admin: build
	JWT_SECRET=$${JWT_SECRET:-workflow-admin-secret} ./server -config $(or $(CONFIG),example/chat-platform/workflow.yaml) --admin

# Clean build artifacts
clean:
	rm -f server
	rm -f example/workflow-example
	rm -rf module/ui_dist/assets module/ui_dist/index.html module/ui_dist/vite.svg
