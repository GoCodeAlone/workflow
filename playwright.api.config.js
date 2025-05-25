// @ts-check
const { defineConfig } = require('@playwright/test');

module.exports = defineConfig({
  testDir: './tests',
  timeout: 30 * 1000,
  expect: {
    timeout: 5000
  },
  reporter: [['line']],
  use: {
    baseURL: 'http://localhost:8080',
  },
  // Skip browser downloads by setting skipInstallBrowsers
  skipInstallBrowsers: true,
  webServer: {
    command: 'go run example/main.go -config example/ui-server-test.yaml',
    port: 8080,
    reuseExistingServer: false,
  },
});