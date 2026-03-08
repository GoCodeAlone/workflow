.PHONY: build build-ui build-go test bench bench-baseline bench-compare lint fmt vet fix install-hooks clean ko-build build-wfctl

# Common benchmark flags
BENCH_FLAGS = -bench=. -benchmem -run=^$$ -timeout=30m

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

# Build wfctl CLI (includes MCP server)
build-wfctl:
	go build -o wfctl ./cmd/wfctl

# Run all tests with race detection
test:
	go test -race ./...

# Run all benchmarks (single iteration for quick local feedback)
bench:
	go test $(BENCH_FLAGS) ./...

# Run benchmarks with multiple iterations and save as baseline for comparison
bench-baseline:
	go test $(BENCH_FLAGS) -count=6 ./... | tee baseline-bench.txt

# Compare current benchmarks against saved baseline (requires baseline-bench.txt)
bench-compare:
	@if [ ! -f baseline-bench.txt ]; then echo "No baseline found. Run 'make bench-baseline' first."; exit 1; fi
	go test $(BENCH_FLAGS) -count=6 ./... | tee current-bench.txt
	benchstat baseline-bench.txt current-bench.txt

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

# Build container image with ko (requires ko: brew install ko)
ko-build:
	KO_DOCKER_REPO=ko.local ko build ./cmd/server --bare --platform=linux/$(shell go env GOARCH)

# Clean build artifacts
clean:
	rm -f server
	rm -f example/workflow-example
	rm -rf module/ui_dist/assets module/ui_dist/index.html module/ui_dist/vite.svg
