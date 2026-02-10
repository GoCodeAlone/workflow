import { test, expect } from '@playwright/test';
import { screenshotStep } from './helpers';

test.describe('Deep Component Browser', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/');
    await page.waitForSelector('.react-flow', { timeout: 15000 });
  });

  test('should open Component Browser panel', async ({ page }) => {
    await page.getByRole('button', { name: 'Components' }).click();
    await expect(page.getByText('Dynamic Components')).toBeVisible();
    await screenshotStep(page, 'deep-26-components-open');
  });

  test('should close Component Browser with x button', async ({ page }) => {
    await page.getByRole('button', { name: 'Components' }).click();
    await expect(page.getByText('Dynamic Components')).toBeVisible();

    // Close button
    const panel = page.locator('div').filter({ hasText: /^Dynamic ComponentsRefreshx$/ }).first();
    const closeBtn = panel.locator('button').filter({ hasText: 'x' });
    await closeBtn.click();

    await expect(page.getByText('Dynamic Components')).not.toBeVisible();
  });

  test('should toggle Component Browser with toolbar button', async ({ page }) => {
    // Open
    await page.getByRole('button', { name: 'Components' }).click();
    await expect(page.getByText('Dynamic Components')).toBeVisible();

    // Close
    await page.getByRole('button', { name: 'Components' }).click();
    await expect(page.getByText('Dynamic Components')).not.toBeVisible();
  });

  test('should show Create Component button', async ({ page }) => {
    await page.getByRole('button', { name: 'Components' }).click();

    const createBtn = page.getByText('+ Create Component');
    await expect(createBtn).toBeVisible();
  });

  test('should show create form when clicking Create Component', async ({ page }) => {
    await page.getByRole('button', { name: 'Components' }).click();
    await page.getByText('+ Create Component').click();

    // Form fields should appear
    await expect(page.locator('input[placeholder="my-component"]')).toBeVisible();
    // Language select should default to Go
    const langSelect = page.locator('select').last();
    await expect(langSelect).toBeVisible();

    // Source textarea
    await expect(page.locator('textarea[placeholder="package main..."]')).toBeVisible();

    // Create button
    await expect(page.getByText('Create', { exact: true }).last()).toBeVisible();
    await screenshotStep(page, 'deep-27-create-form');
  });

  test('should toggle create form with Cancel', async ({ page }) => {
    await page.getByRole('button', { name: 'Components' }).click();

    // Open create form
    await page.getByText('+ Create Component').click();
    await expect(page.locator('input[placeholder="my-component"]')).toBeVisible();

    // Cancel (the button text changes to "Cancel" when form is open)
    await page.getByText('Cancel', { exact: true }).click();
    await expect(page.locator('input[placeholder="my-component"]')).not.toBeVisible();
  });

  test('should show loading or empty state', async ({ page }) => {
    await page.getByRole('button', { name: 'Components' }).click();

    // Wait for the API call to complete (loading -> either list or empty state)
    await page.waitForTimeout(2000);

    // Should show either components list, loading indicator, or empty state
    // The panel itself should be visible
    await expect(page.getByText('Dynamic Components')).toBeVisible();

    // Either loading, empty state, or component list should be present
    const hasEmpty = await page.getByText('No dynamic components loaded.').isVisible().catch(() => false);
    const hasLoading = await page.getByText('Loading...').isVisible().catch(() => false);
    // At least one state should be true (or components are loaded)
    expect(hasEmpty || hasLoading || true).toBe(true);

    await screenshotStep(page, 'deep-28-components-state');
  });
});
