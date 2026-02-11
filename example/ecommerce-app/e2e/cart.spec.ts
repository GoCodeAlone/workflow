import { test, expect } from '@playwright/test';
import { clearState, addProductToCart } from './helpers';

test.describe('Shopping Cart', () => {
  test.beforeEach(async ({ page }) => {
    await clearState(page);
    await page.goto('/#/');
  });

  test('add product to cart updates badge', async ({ page }) => {
    // Click on product
    await page.getByText('Mechanical Keyboard', { exact: true }).click();
    await page.getByRole('button', { name: 'Add to Cart' }).click();

    // Toast appears
    await expect(page.getByText('Mechanical Keyboard added to cart')).toBeVisible();
    // Cart badge shows 1
    await expect(page.getByRole('link', { name: /Cart\s*1/ })).toBeVisible();
  });

  test('cart page shows items with details', async ({ page }) => {
    await addProductToCart(page, 'Mechanical Keyboard');
    await addProductToCart(page, 'Wireless Mouse');

    await page.goto('/#/cart');
    await expect(page.getByRole('heading', { name: 'Your Cart' })).toBeVisible();
    // Use locators within cart items to avoid toast collisions
    await expect(page.locator('.cart-item-name').filter({ hasText: 'Mechanical Keyboard' })).toBeVisible();
    await expect(page.locator('.cart-item-name').filter({ hasText: 'Wireless Mouse' })).toBeVisible();
    await expect(page.locator('.cart-item-price').filter({ hasText: '$149.99' })).toBeVisible();
    await expect(page.locator('.cart-item-price').filter({ hasText: '$49.99' })).toBeVisible();
  });

  test('update quantity with + and - buttons', async ({ page }) => {
    await addProductToCart(page, 'Mechanical Keyboard');
    await page.goto('/#/cart');

    await expect(page.getByRole('heading', { name: 'Your Cart' })).toBeVisible();

    // Click + to increase
    await page.locator('.qty-plus').first().click();
    // Total should update (2 x $149.99 = $299.98)
    await expect(page.locator('.cart-summary-total').getByText('$299.98')).toBeVisible();

    // Click - to decrease back to 1
    await page.locator('.qty-minus').first().click();
    await expect(page.locator('.cart-summary-total').getByText('$149.99')).toBeVisible();
  });

  test('remove item from cart', async ({ page }) => {
    await addProductToCart(page, 'Mechanical Keyboard');
    await addProductToCart(page, 'Wireless Mouse');

    await page.goto('/#/cart');
    // Remove the first item
    await page.locator('.cart-item-remove').first().click();
    await expect(page.getByText('Item removed')).toBeVisible();

    // Only one item should remain
    const cartItems = page.locator('.cart-item');
    await expect(cartItems).toHaveCount(1);
  });

  test('empty cart shows message and browse link', async ({ page }) => {
    await page.goto('/#/cart');
    await expect(page.getByText('Your cart is empty')).toBeVisible();
    await expect(page.getByRole('link', { name: 'Browse Products' })).toBeVisible();
  });
});
