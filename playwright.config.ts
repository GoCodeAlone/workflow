import { defineConfig, devices } from '@playwright/test';

export default defineConfig({
  testDir: './tests/e2e',
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  workers: process.env.CI ? 1 : undefined,
  timeout: 30000, // 30 seconds for overall test timeout in CI
  reporter: [
    ['html', { outputFolder: 'playwright-report' }],
    ['json', { outputFile: 'test-results.json' }]
  ],
  use: {
    baseURL: 'http://localhost:8080',
    trace: 'off', // Disable trace to avoid browser dependencies
    screenshot: 'off', // Disable screenshots to avoid issues
    video: 'off', // Disable video to avoid ffmpeg dependency
    actionTimeout: 10000, // 10 seconds for actions (allow more time for form interactions)
    navigationTimeout: 15000, // 15 seconds for navigation
  },

  projects: [
    {
      name: 'chromium',
      use: { 
        ...devices['Desktop Chrome'],
        channel: 'chrome', // Use system Chrome instead of Playwright's chromium
      },
    },
  ],

  webServer: {
    command: 'go run example/main.go -config example/ui-workflow-config.yaml',
    url: 'http://localhost:8080',
    reuseExistingServer: !process.env.CI,
    timeout: 180 * 1000, // Keep server startup timeout high for CI
    stdout: 'pipe',
    stderr: 'pipe',
  },
});