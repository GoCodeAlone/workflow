import { defineConfig, devices } from '@playwright/test';

// Separate config for trace-related tests to avoid port conflicts.
// Run with: npx playwright test --config=playwright-trace.config.ts e2e/trace.spec.ts
export default defineConfig({
  testDir: './e2e',
  testMatch: 'trace.spec.ts',
  fullyParallel: false,
  forbidOnly: !!process.env.CI,
  retries: 0,
  workers: 1,
  reporter: 'list',
  timeout: 60000,
  use: {
    baseURL: 'http://localhost:5176',
    screenshot: 'on',
    trace: 'on-first-retry',
    video: 'retain-on-failure',
  },
  expect: {
    toHaveScreenshot: { maxDiffPixels: 100 },
  },
  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],
  webServer: {
    command: 'npm run dev -- --port 5176',
    port: 5176,
    reuseExistingServer: !process.env.CI,
    timeout: 60000,
  },
});
