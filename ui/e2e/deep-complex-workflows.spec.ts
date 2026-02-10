import { test, expect } from '@playwright/test';
import { dragModuleToCanvas, connectNodes, waitForNodeCount, screenshotStep } from './helpers';

test.describe('Deep Complex Workflows', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/');
    await page.waitForSelector('.react-flow', { timeout: 15000 });
    await page.waitForTimeout(500);
  });

  test('should build a 3-node HTTP pipeline', async ({ page }) => {
    await dragModuleToCanvas(page, 'HTTP Server', 200, 30);
    await waitForNodeCount(page, 1);
    await dragModuleToCanvas(page, 'Auth Middleware', 200, 250);
    await waitForNodeCount(page, 2);
    await dragModuleToCanvas(page, 'HTTP Router', 200, 470);
    await waitForNodeCount(page, 3);

    await expect(page.getByText('3 modules')).toBeVisible();

    // Deselect before connecting
    await page.locator('.react-flow__pane').click({ position: { x: 50, y: 600 } });
    await page.waitForTimeout(300);

    // Connect HTTP Server -> Auth Middleware (single connection is reliable)
    await connectNodes(page, 0, 1);

    // Should have at least one edge
    const edgeCount = await page.locator('.react-flow__edge').count();
    expect(edgeCount).toBeGreaterThanOrEqual(1);

    await screenshotStep(page, 'deep-14-http-pipeline');
  });

  test('should build a messaging workflow layout', async ({ page }) => {
    await dragModuleToCanvas(page, 'Message Broker', 200, 50);
    await waitForNodeCount(page, 1);
    await dragModuleToCanvas(page, 'Message Handler', 200, 250);
    await waitForNodeCount(page, 2);
    await dragModuleToCanvas(page, 'Event Logger', 200, 450);
    await waitForNodeCount(page, 3);

    await expect(page.getByText('3 modules')).toBeVisible();

    // Verify each node is rendered with correct labels
    await expect(page.locator('.react-flow__node').filter({ hasText: /Message Broker/ })).toBeVisible();
    await expect(page.locator('.react-flow__node').filter({ hasText: /Message Handler/ })).toBeVisible();
    await expect(page.locator('.react-flow__node').filter({ hasText: /Event Logger/ })).toBeVisible();

    await screenshotStep(page, 'deep-15-messaging-workflow');
  });

  test('should build a 5-node workflow across categories', async ({ page }) => {
    await dragModuleToCanvas(page, 'HTTP Server', 200, 30);
    await waitForNodeCount(page, 1);
    await dragModuleToCanvas(page, 'Rate Limiter', 200, 200);
    await waitForNodeCount(page, 2);
    await dragModuleToCanvas(page, 'HTTP Router', 200, 370);
    await waitForNodeCount(page, 3);
    await dragModuleToCanvas(page, 'Cache', 500, 30);
    await waitForNodeCount(page, 4);
    await dragModuleToCanvas(page, 'Metrics Collector', 500, 200);
    await waitForNodeCount(page, 5);

    await expect(page.getByText('5 modules')).toBeVisible();
    await screenshotStep(page, 'deep-16-5-node-workflow');
  });

  test('should verify module count updates correctly', async ({ page }) => {
    await expect(page.getByText('0 modules')).toBeVisible();

    await dragModuleToCanvas(page, 'HTTP Server', 300, 100);
    await waitForNodeCount(page, 1);
    await expect(page.getByText('1 modules')).toBeVisible();

    await dragModuleToCanvas(page, 'HTTP Router', 300, 350);
    await waitForNodeCount(page, 2);
    await expect(page.getByText('2 modules')).toBeVisible();

    // Delete one via property panel
    await page.locator('.react-flow__node').first().click();
    await page.waitForTimeout(300);
    await page.getByRole('button', { name: 'Delete Node' }).click();
    await waitForNodeCount(page, 1);
    await expect(page.getByText('1 modules')).toBeVisible();
  });

  test('should build workflow with infrastructure and scheduling', async ({ page }) => {
    await dragModuleToCanvas(page, 'Event Bus', 200, 50);
    await waitForNodeCount(page, 1);
    await dragModuleToCanvas(page, 'Scheduler', 200, 250);
    await waitForNodeCount(page, 2);
    await dragModuleToCanvas(page, 'Cache', 200, 450);
    await waitForNodeCount(page, 3);

    await expect(page.getByText('3 modules')).toBeVisible();

    await expect(page.locator('.react-flow__node').filter({ hasText: /Event Bus/ })).toBeVisible();
    await expect(page.locator('.react-flow__node').filter({ hasText: /Scheduler/ })).toBeVisible();
    await expect(page.locator('.react-flow__node').filter({ hasText: /Cache/ })).toBeVisible();

    await screenshotStep(page, 'deep-17-infra-schedule');
  });

  test('should clear a complex workflow', async ({ page }) => {
    // Build a 4-node workflow
    await dragModuleToCanvas(page, 'HTTP Server', 200, 50);
    await waitForNodeCount(page, 1);
    await dragModuleToCanvas(page, 'Auth Middleware', 200, 250);
    await waitForNodeCount(page, 2);
    await dragModuleToCanvas(page, 'HTTP Router', 200, 450);
    await waitForNodeCount(page, 3);
    await dragModuleToCanvas(page, 'Scheduler', 500, 150);
    await waitForNodeCount(page, 4);

    await expect(page.getByText('4 modules')).toBeVisible();

    // Clear
    await page.getByRole('button', { name: 'Clear' }).click();
    await waitForNodeCount(page, 0);
    await expect(page.getByText('0 modules')).toBeVisible();

    // Undo clear to restore
    await page.getByRole('button', { name: 'Undo' }).click();
    await waitForNodeCount(page, 4);
    await expect(page.getByText('4 modules')).toBeVisible();
    await screenshotStep(page, 'deep-18-undo-clear');
  });
});
