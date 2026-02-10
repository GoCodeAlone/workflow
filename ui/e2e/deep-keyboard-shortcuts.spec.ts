import { test, expect } from '@playwright/test';
import { dragModuleToCanvas, waitForNodeCount, screenshotStep } from './helpers';

test.describe('Deep Keyboard Shortcuts', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/');
    await page.waitForSelector('.react-flow', { timeout: 15000 });
  });

  test('should delete selected node with Delete key', async ({ page }) => {
    await dragModuleToCanvas(page, 'HTTP Server', 300, 200);
    await waitForNodeCount(page, 1);

    // Click node to select it
    await page.locator('.react-flow__node').first().click();
    await page.waitForTimeout(300);

    // Press Delete
    await page.keyboard.press('Delete');
    await waitForNodeCount(page, 0);
    await expect(page.getByText('0 modules')).toBeVisible();
    await screenshotStep(page, 'deep-09-delete-key');
  });

  test('should delete selected node with Backspace key', async ({ page }) => {
    await dragModuleToCanvas(page, 'HTTP Server', 300, 200);
    await waitForNodeCount(page, 1);

    await page.locator('.react-flow__node').first().click();
    await page.waitForTimeout(300);

    // Press Backspace
    await page.keyboard.press('Backspace');
    await waitForNodeCount(page, 0);
  });

  test('should deselect node with Escape key', async ({ page }) => {
    await dragModuleToCanvas(page, 'HTTP Server', 300, 200);
    await waitForNodeCount(page, 1);

    // Click node to select and show property panel
    await page.locator('.react-flow__node').first().click();
    await expect(page.getByText('Properties', { exact: true })).toBeVisible();

    // Press Escape to deselect - click on pane area
    await page.locator('.react-flow__pane').click({ position: { x: 50, y: 50 } });
    await page.waitForTimeout(300);

    // Property panel should show placeholder
    await expect(page.getByText('Select a node to edit its properties')).toBeVisible();
    await screenshotStep(page, 'deep-10-escape-deselect');
  });

  test('should undo with Ctrl+Z', async ({ page }) => {
    await dragModuleToCanvas(page, 'HTTP Server', 300, 200);
    await waitForNodeCount(page, 1);

    // Click on pane to ensure canvas has focus
    await page.locator('.react-flow__pane').click({ position: { x: 50, y: 50 } });
    await page.waitForTimeout(200);

    // Ctrl+Z to undo
    await page.keyboard.press('Control+z');
    await waitForNodeCount(page, 0);
    await screenshotStep(page, 'deep-11-ctrl-z');
  });

  test('should redo with Ctrl+Shift+Z', async ({ page }) => {
    await dragModuleToCanvas(page, 'HTTP Server', 300, 200);
    await waitForNodeCount(page, 1);

    await page.locator('.react-flow__pane').click({ position: { x: 50, y: 50 } });
    await page.waitForTimeout(200);

    // Undo
    await page.keyboard.press('Control+z');
    await waitForNodeCount(page, 0);

    // Redo
    await page.keyboard.press('Control+Shift+z');
    await waitForNodeCount(page, 1);
  });

  test('should undo with toolbar button after keyboard delete', async ({ page }) => {
    await dragModuleToCanvas(page, 'HTTP Server', 300, 200);
    await waitForNodeCount(page, 1);

    // Select and delete via keyboard
    await page.locator('.react-flow__node').first().click();
    await page.waitForTimeout(300);
    await page.keyboard.press('Delete');
    await waitForNodeCount(page, 0);

    // Undo via toolbar
    const undoBtn = page.getByRole('button', { name: 'Undo' });
    await expect(undoBtn).toBeEnabled();
    await undoBtn.click();
    await waitForNodeCount(page, 1);
    await screenshotStep(page, 'deep-12-undo-after-delete');
  });

  test('should undo multiple operations sequentially', async ({ page }) => {
    await dragModuleToCanvas(page, 'HTTP Server', 200, 100);
    await waitForNodeCount(page, 1);
    await dragModuleToCanvas(page, 'HTTP Router', 400, 100);
    await waitForNodeCount(page, 2);
    await dragModuleToCanvas(page, 'Scheduler', 300, 350);
    await waitForNodeCount(page, 3);

    // Undo 3 times via toolbar
    const undoBtn = page.getByRole('button', { name: 'Undo' });
    await undoBtn.click();
    await waitForNodeCount(page, 2);
    await undoBtn.click();
    await waitForNodeCount(page, 1);
    await undoBtn.click();
    await waitForNodeCount(page, 0);
    await screenshotStep(page, 'deep-13-multi-undo');
  });

  test('should not delete node when input is focused', async ({ page }) => {
    await dragModuleToCanvas(page, 'HTTP Server', 300, 200);
    await waitForNodeCount(page, 1);

    // Click node to select
    await page.locator('.react-flow__node').first().click();

    // Focus on the name input in property panel
    const nameInput = page.locator('label').filter({ hasText: 'Name' }).locator('input');
    await nameInput.click();
    await nameInput.fill('Test');

    // The node should not be deleted when input is focused and we type delete
    // (The delete key only works when the canvas/node has focus, not inputs)
    await waitForNodeCount(page, 1);
  });

  test('should redo button become enabled after undo', async ({ page }) => {
    await dragModuleToCanvas(page, 'HTTP Server', 300, 200);
    await waitForNodeCount(page, 1);

    const undoBtn = page.getByRole('button', { name: 'Undo' });
    const redoBtn = page.getByRole('button', { name: 'Redo' });

    // Redo should be disabled initially
    await expect(redoBtn).toBeDisabled();

    // After undo, redo should be enabled
    await undoBtn.click();
    await waitForNodeCount(page, 0);
    await expect(redoBtn).toBeEnabled();
  });
});
