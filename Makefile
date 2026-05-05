.PHONY: build build-ui build-go test bench bench-baseline bench-compare lint fmt vet fix install-hooks clean ko-build build-wfctl build-iac-codemod migrate-providers

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

# Build the iac-codemod CLI (W-8 / cmd/iac-codemod). GOWORK=off keeps
# the build self-contained: contributors with a workspace go.work file
# that doesn't include this module shouldn't have to amend their
# environment to run `make migrate-providers`.
build-iac-codemod:
	GOWORK=off go build -o iac-codemod ./cmd/iac-codemod

# Workspace-wide IaC migration runner (W-8 / T8.6).
#
# Runs `iac-codemod lint -dry-run` against the AWS, GCP, and Azure plugin
# repos as advisory-only checks. The plugins themselves stay un-migrated
# at v1 (per plan §W-8: "AWS/GCP/Azure plugins are run advisory-only (no
# `-fix`); their reports are filed as GitHub issues against the
# respective plugin repos for activation-time triage"). For DO, run the
# refactor-* modes manually with `-fix` against the workspace's DO
# checkout — that migration is the subject of P-DO and is intentionally
# excluded from this target's mechanical sweep.
#
# Provider paths are sibling-repo defaults; override on the command line:
#
#	make migrate-providers AWS=/path/to/workflow-plugin-aws \
#	                       GCP=/path/to/workflow-plugin-gcp \
#	                       AZURE=/path/to/workflow-plugin-azure
AWS ?= ../workflow-plugin-aws
GCP ?= ../workflow-plugin-gcp
AZURE ?= ../workflow-plugin-azure

migrate-providers: build-iac-codemod
	@# iac-codemod lint exit-code semantics (review round-5 finding #7):
	@#   0 = clean / 1 = advisory findings (continue) / 2 = parse errors (fail).
	@# Naive `|| true` would swallow real execution failures alongside the
	@# expected advisory findings; gate on exit code 1 specifically so a
	@# parse-error or unknown-flag (>=2) still fails the target.
	@echo "==> Running iac-codemod lint (advisory) against AWS plugin: $(AWS)"
	@if [ -d "$(AWS)" ]; then ./iac-codemod lint -dry-run "$(AWS)"; ec=$$?; if [ $$ec -ne 0 ] && [ $$ec -ne 1 ]; then echo "  iac-codemod lint failed (exit=$$ec)"; exit $$ec; fi; else echo "  (skipping: $(AWS) not found)"; fi
	@echo "==> Running iac-codemod lint (advisory) against GCP plugin: $(GCP)"
	@if [ -d "$(GCP)" ]; then ./iac-codemod lint -dry-run "$(GCP)"; ec=$$?; if [ $$ec -ne 0 ] && [ $$ec -ne 1 ]; then echo "  iac-codemod lint failed (exit=$$ec)"; exit $$ec; fi; else echo "  (skipping: $(GCP) not found)"; fi
	@echo "==> Running iac-codemod lint (advisory) against Azure plugin: $(AZURE)"
	@if [ -d "$(AZURE)" ]; then ./iac-codemod lint -dry-run "$(AZURE)"; ec=$$?; if [ $$ec -ne 0 ] && [ $$ec -ne 1 ]; then echo "  iac-codemod lint failed (exit=$$ec)"; exit $$ec; fi; else echo "  (skipping: $(AZURE) not found)"; fi
	@echo "==> migrate-providers complete (advisory-only; no files mutated)"

# Clean build artifacts
clean:
	rm -f server
	rm -f wfctl
	rm -f iac-codemod
	rm -f example/workflow-example
	rm -rf module/ui_dist/assets module/ui_dist/index.html module/ui_dist/vite.svg
