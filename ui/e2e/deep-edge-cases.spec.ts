import { test, expect } from '@playwright/test';
import { dragModuleToCanvas, connectNodes, waitForNodeCount, screenshotStep } from './helpers';

test.describe('Deep Edge Cases', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/');
    await page.waitForSelector('.react-flow', { timeout: 15000 });
    await page.waitForTimeout(500);
  });

  test('should handle rapid undo/redo', async ({ page }) => {
    // Add 3 nodes
    await dragModuleToCanvas(page, 'HTTP Server', 200, 100);
    await waitForNodeCount(page, 1);
    await dragModuleToCanvas(page, 'HTTP Router', 400, 100);
    await waitForNodeCount(page, 2);
    await dragModuleToCanvas(page, 'Scheduler', 300, 350);
    await waitForNodeCount(page, 3);

    const undoBtn = page.getByRole('button', { name: 'Undo' });
    const redoBtn = page.getByRole('button', { name: 'Redo' });

    // Rapid undo
    await undoBtn.click();
    await undoBtn.click();
    await undoBtn.click();
    await waitForNodeCount(page, 0);

    // Rapid redo
    await redoBtn.click();
    await redoBtn.click();
    await redoBtn.click();
    await waitForNodeCount(page, 3);

    await screenshotStep(page, 'deep-29-rapid-undo-redo');
  });

  test('should delete a node and verify removal', async ({ page }) => {
    await dragModuleToCanvas(page, 'HTTP Server', 200, 100);
    await waitForNodeCount(page, 1);
    await dragModuleToCanvas(page, 'Auth Middleware', 200, 350);
    await waitForNodeCount(page, 2);

    await expect(page.getByText('2 modules')).toBeVisible();

    // Delete the first node
    await page.locator('.react-flow__node').first().click();
    await page.waitForTimeout(300);
    await page.getByRole('button', { name: 'Delete Node' }).click();

    await waitForNodeCount(page, 1);
    await expect(page.getByText('1 modules')).toBeVisible();

    // Delete the remaining node
    await page.locator('.react-flow__node').first().click();
    await page.waitForTimeout(300);
    await page.getByRole('button', { name: 'Delete Node' }).click();

    await waitForNodeCount(page, 0);
    await expect(page.getByText('0 modules')).toBeVisible();
    await screenshotStep(page, 'deep-30-delete-all');
  });

  test('should handle adding many nodes', async ({ page }) => {
    // Add 8 nodes
    const modules = [
      'HTTP Server', 'HTTP Router', 'Auth Middleware', 'Rate Limiter',
      'Message Broker', 'Scheduler', 'Cache', 'Event Logger',
    ];
    const positions = [
      [150, 30], [400, 30], [150, 200], [400, 200],
      [150, 370], [400, 370], [150, 540], [400, 540],
    ];

    for (let i = 0; i < modules.length; i++) {
      await dragModuleToCanvas(page, modules[i], positions[i][0], positions[i][1]);
      await waitForNodeCount(page, i + 1);
    }

    await expect(page.getByText('8 modules')).toBeVisible();
    await screenshotStep(page, 'deep-31-many-nodes');
  });

  test('should clear and immediately add new nodes', async ({ page }) => {
    await dragModuleToCanvas(page, 'HTTP Server', 300, 200);
    await waitForNodeCount(page, 1);
    await dragModuleToCanvas(page, 'HTTP Router', 300, 400);
    await waitForNodeCount(page, 2);

    // Clear
    await page.getByRole('button', { name: 'Clear' }).click();
    await waitForNodeCount(page, 0);

    // Immediately add new nodes
    await dragModuleToCanvas(page, 'Scheduler', 300, 200);
    await waitForNodeCount(page, 1);
    await expect(page.getByText('1 modules')).toBeVisible();
  });

  test('should handle double-click on node', async ({ page }) => {
    await dragModuleToCanvas(page, 'HTTP Server', 300, 200);
    await waitForNodeCount(page, 1);

    // Double-click should still select node
    const node = page.locator('.react-flow__node').first();
    await node.dblclick();
    await page.waitForTimeout(300);

    // Property panel should show
    await expect(page.getByText('Properties', { exact: true })).toBeVisible();
  });

  test('should not duplicate modules when dragging same type twice', async ({ page }) => {
    await dragModuleToCanvas(page, 'HTTP Server', 200, 200);
    await waitForNodeCount(page, 1);
    await dragModuleToCanvas(page, 'HTTP Server', 400, 200);
    await waitForNodeCount(page, 2);

    // Both should be on canvas with different IDs
    await expect(page.getByText('2 modules')).toBeVisible();

    // Both nodes should have the http.server type
    const nodes = page.locator('.react-flow__node');
    await expect(nodes).toHaveCount(2);
  });

  test('should preserve node positions after undo/redo', async ({ page }) => {
    await dragModuleToCanvas(page, 'HTTP Server', 200, 100);
    await waitForNodeCount(page, 1);

    // Get initial position
    const node = page.locator('.react-flow__node').first();
    const initialBox = await node.boundingBox();

    // Add another and undo
    await dragModuleToCanvas(page, 'HTTP Router', 400, 300);
    await waitForNodeCount(page, 2);

    await page.getByRole('button', { name: 'Undo' }).click();
    await waitForNodeCount(page, 1);

    // Position should be preserved
    const afterBox = await node.boundingBox();
    expect(afterBox).toBeTruthy();
    expect(initialBox).toBeTruthy();
    // Positions may not be pixel-perfect, but should be in the same area
    expect(Math.abs(afterBox!.x - initialBox!.x)).toBeLessThan(5);
    expect(Math.abs(afterBox!.y - initialBox!.y)).toBeLessThan(5);
  });

  test('should handle selecting then deselecting by clicking pane', async ({ page }) => {
    await dragModuleToCanvas(page, 'HTTP Server', 300, 200);
    await waitForNodeCount(page, 1);

    // Select
    await page.locator('.react-flow__node').first().click();
    await expect(page.getByText('Properties', { exact: true })).toBeVisible();

    // Deselect by clicking canvas pane
    await page.locator('.react-flow__pane').click({ position: { x: 50, y: 50 } });
    await page.waitForTimeout(300);

    await expect(page.getByText('Select a node to edit its properties')).toBeVisible();
    await screenshotStep(page, 'deep-32-deselect-pane');
  });
});
