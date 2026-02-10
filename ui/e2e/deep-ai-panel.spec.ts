import { test, expect } from '@playwright/test';
import { screenshotStep } from './helpers';

test.describe('Deep AI Panel', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/');
    await page.waitForSelector('.react-flow', { timeout: 15000 });
  });

  test('should open AI Copilot panel', async ({ page }) => {
    await page.getByRole('button', { name: 'AI Copilot' }).click();
    await expect(page.getByText('AI Copilot').last()).toBeVisible();
    await expect(page.getByText('Describe your workflow')).toBeVisible();
    await screenshotStep(page, 'deep-23-ai-panel-open');
  });

  test('should close AI Copilot panel with x button', async ({ page }) => {
    await page.getByRole('button', { name: 'AI Copilot' }).click();
    await expect(page.getByText('Describe your workflow')).toBeVisible();

    // Click the close button (x) in the panel header
    // The AI panel header has a close button with text "x"
    const panel = page.locator('div').filter({ hasText: /^AI Copilotx$/ }).first();
    const closeBtn = panel.locator('button').filter({ hasText: 'x' });
    await closeBtn.click();

    await expect(page.getByText('Describe your workflow')).not.toBeVisible();
  });

  test('should toggle AI panel with toolbar button', async ({ page }) => {
    // Open
    await page.getByRole('button', { name: 'AI Copilot' }).click();
    await expect(page.getByText('Describe your workflow')).toBeVisible();

    // Toggle off by clicking toolbar button again
    await page.getByRole('button', { name: 'AI Copilot' }).click();
    await expect(page.getByText('Describe your workflow')).not.toBeVisible();
  });

  test('should show textarea for workflow description', async ({ page }) => {
    await page.getByRole('button', { name: 'AI Copilot' }).click();

    const textarea = page.locator('textarea[placeholder*="REST API"]');
    await expect(textarea).toBeVisible();
    await textarea.fill('A simple web server');
    await expect(textarea).toHaveValue('A simple web server');
  });

  test('should show Generate Workflow button', async ({ page }) => {
    await page.getByRole('button', { name: 'AI Copilot' }).click();

    const generateBtn = page.getByText('Generate Workflow', { exact: true });
    await expect(generateBtn).toBeVisible();
  });

  test('should show quick start suggestion chips', async ({ page }) => {
    await page.getByRole('button', { name: 'AI Copilot' }).click();

    await expect(page.getByText('Quick start')).toBeVisible();
    await expect(page.getByText('REST API with auth and rate limiting')).toBeVisible();
    await expect(page.getByText('Event-driven microservice')).toBeVisible();
    await expect(page.getByText('HTTP proxy with logging')).toBeVisible();
    await expect(page.getByText('Scheduled data pipeline')).toBeVisible();
    await expect(page.getByText('WebSocket chat backend')).toBeVisible();
    await screenshotStep(page, 'deep-24-ai-quick-start');
  });

  test('should populate textarea when clicking quick suggestion', async ({ page }) => {
    await page.getByRole('button', { name: 'AI Copilot' }).click();

    // Click a quick suggestion
    await page.getByText('REST API with auth and rate limiting').click();

    const textarea = page.locator('textarea[placeholder*="REST API"]');
    await expect(textarea).toHaveValue('REST API with auth and rate limiting');
  });

  test('should show Explore suggestions section', async ({ page }) => {
    await page.getByRole('button', { name: 'AI Copilot' }).click();

    await expect(page.getByText('Explore suggestions')).toBeVisible();
    const useCaseInput = page.locator('input[placeholder*="Use case"]');
    await expect(useCaseInput).toBeVisible();

    const suggestBtn = page.getByText('Suggest', { exact: true });
    await expect(suggestBtn).toBeVisible();
    await screenshotStep(page, 'deep-25-ai-explore');
  });

  test('should have Generate button disabled when textarea is empty', async ({ page }) => {
    await page.getByRole('button', { name: 'AI Copilot' }).click();

    // Ensure textarea is empty
    const textarea = page.locator('textarea[placeholder*="REST API"]');
    await textarea.fill('');

    const generateBtn = page.getByText('Generate Workflow', { exact: true });
    // The button should be disabled (the button has disabled={loading || !intent.trim()})
    await expect(generateBtn).toBeDisabled();
  });
});
