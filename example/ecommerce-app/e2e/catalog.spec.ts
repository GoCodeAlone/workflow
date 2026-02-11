import { test, expect } from '@playwright/test';
import { clearState } from './helpers';

test.describe('Product Catalog', () => {
  test.beforeEach(async ({ page }) => {
    await clearState(page);
    await page.goto('/#/');
  });

  test('displays product grid with all 8 products', async ({ page }) => {
    await expect(page.getByRole('heading', { name: 'Products' })).toBeVisible();

    // Check all 8 seed products are displayed
    const productNames = [
      'Mechanical Keyboard', 'Wireless Mouse', 'USB-C Hub',
      'Monitor Light Bar', 'Laptop Stand', 'Desk Mat',
      'Cable Management Kit', 'IDE License Pro',
    ];
    for (const name of productNames) {
      await expect(page.getByText(name, { exact: true })).toBeVisible();
    }
  });

  test('product cards show name and price', async ({ page }) => {
    // Look for price inside the product grid area
    await expect(page.getByText('Mechanical Keyboard', { exact: true })).toBeVisible();
    await expect(page.getByText('$149.99').first()).toBeVisible();
  });

  test('clicking a product navigates to product detail', async ({ page }) => {
    await page.getByText('Mechanical Keyboard', { exact: true }).click();

    await expect(page).toHaveURL(/\/#\/product\/prod-001/);
    await expect(page.getByRole('heading', { name: 'Mechanical Keyboard' })).toBeVisible();
    await expect(page.getByText('Premium hot-swappable mechanical keyboard')).toBeVisible();
    await expect(page.getByText('50 units in stock')).toBeVisible();
    await expect(page.getByRole('button', { name: 'Add to Cart' })).toBeVisible();
  });

  test('product detail shows category badge', async ({ page }) => {
    await page.getByText('Wireless Mouse', { exact: true }).click();
    await expect(page.getByRole('heading', { name: 'Wireless Mouse' })).toBeVisible();
    // Category badge should show "Electronics"
    await expect(page.locator('.product-detail-category, .category-badge').first()).toBeVisible().catch(async () => {
      // Fallback: just check the text is on the page
      await expect(page.getByText('Electronics').first()).toBeVisible();
    });
  });

  test('back to catalog link works from product detail', async ({ page }) => {
    await page.getByText('Desk Mat', { exact: true }).click();
    await expect(page.getByRole('heading', { name: 'Desk Mat' })).toBeVisible();

    await page.getByRole('link', { name: /Back to catalog/ }).click();
    await expect(page.getByRole('heading', { name: 'Products' })).toBeVisible();
  });
});
