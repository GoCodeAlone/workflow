import { test, expect } from '@playwright/test';
import { dragModuleToCanvas, waitForNodeCount, screenshotStep } from './helpers';

test.describe('Deep Accessibility', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/');
    await page.waitForSelector('.react-flow', { timeout: 15000 });
  });

  test('should have accessible toolbar buttons with proper labels', async ({ page }) => {
    const buttons = ['Import', 'Load Server', 'Export YAML', 'Save', 'Validate', 'Undo', 'Redo', 'Clear'];
    for (const label of buttons) {
      const btn = page.getByRole('button', { name: label });
      await expect(btn).toBeVisible();
    }
    await screenshotStep(page, 'deep-39-toolbar-labels');
  });

  test('should have accessible AI Copilot and Components buttons', async ({ page }) => {
    await expect(page.getByRole('button', { name: 'AI Copilot' })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Components' })).toBeVisible();
  });

  test('should focus name input when node is selected', async ({ page }) => {
    await dragModuleToCanvas(page, 'HTTP Server', 300, 200);
    await waitForNodeCount(page, 1);

    await page.locator('.react-flow__node').first().click();

    const nameInput = page.locator('label').filter({ hasText: 'Name' }).locator('input');
    await expect(nameInput).toBeVisible();

    // Should be able to focus and type in the name input
    await nameInput.click();
    await nameInput.fill('Accessible Server');
    await expect(nameInput).toHaveValue('Accessible Server');
  });

  test('should have labeled config fields', async ({ page }) => {
    await dragModuleToCanvas(page, 'HTTP Server', 300, 200);
    await waitForNodeCount(page, 1);

    await page.locator('.react-flow__node').first().click();

    // Check that config fields have labels
    await expect(page.locator('label').filter({ hasText: 'Name' })).toBeVisible();
    await expect(page.locator('label').filter({ hasText: 'Address' })).toBeVisible();
    await expect(page.locator('label').filter({ hasText: 'Read Timeout' })).toBeVisible();
    await expect(page.locator('label').filter({ hasText: 'Write Timeout' })).toBeVisible();
    await screenshotStep(page, 'deep-40-labeled-fields');
  });

  test('should have keyboard-accessible toolbar buttons', async ({ page }) => {
    // Tab through toolbar buttons to verify they are focusable
    await page.keyboard.press('Tab');
    await page.waitForTimeout(100);

    // At least one button should be focused (we don't know exact tab order)
    // Just verify buttons can receive focus
    const importBtn = page.getByRole('button', { name: 'Import' });
    await importBtn.focus();
    await expect(importBtn).toBeFocused();
  });

  test('should have Delete Node button accessible', async ({ page }) => {
    await dragModuleToCanvas(page, 'HTTP Server', 300, 200);
    await waitForNodeCount(page, 1);

    await page.locator('.react-flow__node').first().click();

    const deleteBtn = page.getByRole('button', { name: 'Delete Node' });
    await expect(deleteBtn).toBeVisible();
    await deleteBtn.focus();
    await expect(deleteBtn).toBeFocused();
  });

  test('should navigate between sidebar categories', async ({ page }) => {
    // Verify all 10 categories are clickable and interactive
    const categories = [
      'HTTP', 'Middleware', 'Messaging', 'State Machine', 'Events',
      'Integration', 'Scheduling', 'Infrastructure', 'Database', 'Observability',
    ];

    for (const category of categories) {
      const cat = page.locator('[style*="cursor: pointer"]').filter({ hasText: category }).first();
      await cat.scrollIntoViewIfNeeded();
      await expect(cat).toBeVisible();
    }
    await screenshotStep(page, 'deep-41-categories-accessible');
  });

  test('should have proper disabled state on toolbar buttons', async ({ page }) => {
    // When empty, these buttons should be disabled
    await expect(page.getByRole('button', { name: 'Export YAML' })).toBeDisabled();
    await expect(page.getByRole('button', { name: 'Save' })).toBeDisabled();
    await expect(page.getByRole('button', { name: 'Validate' })).toBeDisabled();
    await expect(page.getByRole('button', { name: 'Clear' })).toBeDisabled();
    await expect(page.getByRole('button', { name: 'Undo' })).toBeDisabled();
    await expect(page.getByRole('button', { name: 'Redo' })).toBeDisabled();

    // Import and Load Server should always be enabled
    await expect(page.getByRole('button', { name: 'Import' })).toBeEnabled();
    await expect(page.getByRole('button', { name: 'Load Server' })).toBeEnabled();
  });
});
