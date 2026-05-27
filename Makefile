.PHONY: build build-ui build-go test bench bench-baseline bench-compare lint fmt vet fix install-hooks clean ko-build build-wfctl vendor-infra-proto test-integration-admin

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

# Run golangci-lint + workflow#699 proto guard (re-introduction of rpc
# Apply on the IaCProviderRequired service is a regression — guarded by
# CI so a future PR can't silently restore the deleted dispatch path).
lint:
	golangci-lint run --timeout=5m
	@if grep -qE '^[[:space:]]*rpc Apply[[:space:]]*\(' plugin/external/proto/iac.proto; then \
		echo "workflow#699: rpc Apply re-introduced in iac.proto; see decisions/0024-iac-typed-force-cutover.md"; \
		exit 1; \
	else \
		echo "workflow#699 guard: rpc Apply correctly absent"; \
	fi

# Run the T17 host-module integration test that exercises the live
# workflow-plugin-admin gRPC plugin subprocess. The test itself
# (module/infra_admin_integration_test.go) probes for the sibling
# repo at ../workflow-plugin-admin and skips when absent — this
# target makes the dependency explicit + lets CI pass an env var
# to point at a pre-checked-out clone. Per
# docs/plans/2026-05-27-infra-admin-dynamic.md Task 17.
#
# Usage:
#   make test-integration-admin                    # uses ../workflow-plugin-admin
#   WORKFLOW_PLUGIN_ADMIN_PATH=/path make ...      # explicit override
test-integration-admin:
	@if [ ! -f "$${WORKFLOW_PLUGIN_ADMIN_PATH:-../workflow-plugin-admin}/go.mod" ]; then \
		echo "workflow-plugin-admin not found at $${WORKFLOW_PLUGIN_ADMIN_PATH:-../workflow-plugin-admin}; set WORKFLOW_PLUGIN_ADMIN_PATH or checkout the sibling repo"; \
		exit 1; \
	fi
	GOWORK=off go test -run TestInfraAdmin_IntegrationWithLiveAdminPlugin -v ./module/

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

# Refresh the vendored workflow-plugin-infra proto descriptor used by
# the FieldSpec catalog parity test (iac/admin/catalog/
# catalog_proto_parity_test.go). Run on every minor upstream
# workflow-plugin-infra release; then update the `Source version:`
# header inside iac/admin/testdata/infra.proto to match the new tag.
#
# Assumes workflow-plugin-infra is checked out as a workspace sibling
# (../workflow-plugin-infra) per the workspace convention.
vendor-infra-proto:
	@if [ ! -f ../workflow-plugin-infra/internal/contracts/infra.proto ]; then \
		echo "vendor-infra-proto: ../workflow-plugin-infra/internal/contracts/infra.proto not found"; \
		exit 1; \
	fi
	@printf '// Vendored from GoCodeAlone/workflow-plugin-infra/internal/contracts/infra.proto\n// Source version: TODO-update-tag (sourced %s)\n// Refresh via: make vendor-infra-proto\n// Drift detection: catalog_proto_parity_test.go\n\n' "$$(date +%Y-%m-%d)" > iac/admin/testdata/infra.proto
	@cat ../workflow-plugin-infra/internal/contracts/infra.proto >> iac/admin/testdata/infra.proto
	@echo "Vendored infra.proto refreshed at iac/admin/testdata/infra.proto."
	@echo "  -> update the 'Source version:' header to the upstream tag now."

# Clean build artifacts
clean:
	rm -f server
	rm -f wfctl
	rm -f example/workflow-example
	rm -rf module/ui_dist/assets module/ui_dist/index.html module/ui_dist/vite.svg
