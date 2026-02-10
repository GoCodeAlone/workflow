import { test, expect } from '@playwright/test';
import { dragModuleToCanvas, connectNodes, waitForNodeCount, screenshotStep } from './helpers';

test.describe('Deep Visual Regression', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/');
    await page.waitForSelector('.react-flow', { timeout: 15000 });
    await page.waitForTimeout(500);
  });

  test('should capture empty state screenshot', async ({ page }) => {
    await screenshotStep(page, 'deep-42-empty-state');
    // Verify key elements visible
    await expect(page.getByText('Workflow Editor')).toBeVisible();
    await expect(page.getByText('Modules', { exact: true })).toBeVisible();
    await expect(page.getByText('Select a node to edit its properties')).toBeVisible();
  });

  test('should capture single node state screenshot', async ({ page }) => {
    await dragModuleToCanvas(page, 'HTTP Server', 300, 200);
    await waitForNodeCount(page, 1);
    await screenshotStep(page, 'deep-43-single-node');
  });

  test('should capture property panel screenshot', async ({ page }) => {
    await dragModuleToCanvas(page, 'HTTP Server', 300, 200);
    await waitForNodeCount(page, 1);
    await page.locator('.react-flow__node').first().click();
    await expect(page.getByText('Properties', { exact: true })).toBeVisible();
    await screenshotStep(page, 'deep-44-property-panel');
  });

  test('should capture connected workflow screenshot', async ({ page }) => {
    await dragModuleToCanvas(page, 'HTTP Server', 200, 50);
    await waitForNodeCount(page, 1);
    await dragModuleToCanvas(page, 'Auth Middleware', 200, 400);
    await waitForNodeCount(page, 2);

    await page.locator('.react-flow__pane').click({ position: { x: 50, y: 600 } });
    await page.waitForTimeout(300);

    await connectNodes(page, 0, 1);
    // Edge may or may not appear depending on ReactFlow timing
    await page.waitForTimeout(500);

    await screenshotStep(page, 'deep-45-connected-workflow');
  });

  test('should capture AI panel open state', async ({ page }) => {
    await page.getByRole('button', { name: 'AI Copilot' }).click();
    await expect(page.getByText('Describe your workflow')).toBeVisible();
    await screenshotStep(page, 'deep-46-ai-panel-open');
  });

  test('should capture multi-node complex layout', async ({ page }) => {
    await dragModuleToCanvas(page, 'HTTP Server', 150, 30);
    await waitForNodeCount(page, 1);
    await dragModuleToCanvas(page, 'Rate Limiter', 400, 30);
    await waitForNodeCount(page, 2);
    await dragModuleToCanvas(page, 'HTTP Router', 150, 220);
    await waitForNodeCount(page, 3);
    await dragModuleToCanvas(page, 'Message Broker', 400, 220);
    await waitForNodeCount(page, 4);
    await dragModuleToCanvas(page, 'Scheduler', 150, 410);
    await waitForNodeCount(page, 5);
    await dragModuleToCanvas(page, 'Metrics Collector', 400, 410);
    await waitForNodeCount(page, 6);

    await expect(page.getByText('6 modules')).toBeVisible();
    await screenshotStep(page, 'deep-47-complex-layout');
  });
});
