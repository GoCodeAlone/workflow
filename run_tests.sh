#!/bin/bash

export PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1

# Build the Go code first
go build ./...

# Run the API tests without requiring browser download
npx playwright test tests/ui/workflow-ui-server.test.js --config playwright.api.config.js