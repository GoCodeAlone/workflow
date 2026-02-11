import { test, expect } from '@playwright/test';
import { clearState, uniqueEmail } from './helpers';

test.describe('End-to-End Flows', () => {
  test.beforeEach(async ({ page }) => {
    await clearState(page);
  });

  test('full flow: register, browse, add to cart, checkout, view order', async ({ page }) => {
    const email = uniqueEmail();

    // 1. Start at catalog (unauthenticated)
    await page.goto('/#/');
    await expect(page.getByRole('heading', { name: 'Products' })).toBeVisible();
    await expect(page.getByRole('link', { name: 'Login' })).toBeVisible();

    // 2. Register
    await page.getByRole('link', { name: 'Login' }).click();
    await page.getByRole('link', { name: 'Create one' }).click();
    await expect(page.getByRole('heading', { name: 'Create account' })).toBeVisible();
    await page.getByLabel('Full name').fill('E2E Shopper');
    await page.getByLabel('Email').fill(email);
    await page.getByLabel('Password').fill('testpassword');
    await page.getByRole('button', { name: 'Create Account' }).click();
    await expect(page.getByRole('link', { name: 'Profile' })).toBeVisible({ timeout: 10000 });

    // 3. Browse products
    await expect(page.getByText('Mechanical Keyboard', { exact: true })).toBeVisible();

    // 4. Add Mechanical Keyboard to cart
    await page.getByText('Mechanical Keyboard', { exact: true }).click();
    await expect(page.getByRole('heading', { name: 'Mechanical Keyboard' })).toBeVisible();
    await page.getByRole('button', { name: 'Add to Cart' }).click();
    await expect(page.getByText('Mechanical Keyboard added to cart')).toBeVisible();

    // 5. Add Wireless Mouse to cart
    await page.getByRole('link', { name: /Back to catalog/ }).click();
    await page.getByText('Wireless Mouse', { exact: true }).click();
    await page.getByRole('button', { name: 'Add to Cart' }).click();
    await expect(page.getByText('Wireless Mouse added to cart')).toBeVisible();
    // Wait for toast to start fading before navigating to cart
    await page.waitForTimeout(500);

    // 6. Check cart (should show 2 items)
    await page.getByRole('link', { name: /Cart/ }).click();
    await expect(page.getByRole('heading', { name: 'Your Cart' })).toBeVisible();
    await expect(page.locator('.cart-item-name').filter({ hasText: 'Mechanical Keyboard' })).toBeVisible();
    await expect(page.locator('.cart-item-name').filter({ hasText: 'Wireless Mouse' })).toBeVisible();

    // 7. Proceed to checkout
    await page.getByRole('link', { name: 'Proceed to Checkout' }).click();
    await expect(page.getByRole('heading', { name: 'Checkout' })).toBeVisible();
    await page.getByLabel('Street Address').fill('1 Infinite Loop');
    await page.getByLabel('City').fill('Cupertino');
    await page.getByLabel('State').fill('CA');
    await page.getByLabel('ZIP Code').fill('95014');
    await page.getByRole('button', { name: 'Place Order' }).click();

    // 8. Verify order placed
    await expect(page.getByText('Order placed successfully!')).toBeVisible({ timeout: 10000 });
    await expect(page).toHaveURL(/\/#\/orders\/\w+/);
    await expect(page.getByText(/Order #/)).toBeVisible();

    // 9. Check orders list
    await page.goto('/#/orders');
    await expect(page.getByRole('heading', { name: 'Your Orders' })).toBeVisible();
    await expect(page.locator('.order-card').first()).toBeVisible({ timeout: 10000 });
  });

  test('unauthenticated checkout redirects to login then back after auth', async ({ page }) => {
    // Add item to cart (no auth needed)
    await page.goto('/#/');
    await page.getByText('IDE License Pro', { exact: true }).click();
    await page.getByRole('button', { name: 'Add to Cart' }).click();

    // Try to go to checkout
    await page.goto('/#/checkout');
    // Should redirect to login
    await expect(page.getByRole('heading', { name: 'Welcome back' })).toBeVisible({ timeout: 5000 });

    // Register
    const email = uniqueEmail();
    await page.getByRole('link', { name: 'Create one' }).click();
    await page.getByLabel('Full name').fill('Redirect User');
    await page.getByLabel('Email').fill(email);
    await page.getByLabel('Password').fill('password123');
    await page.getByRole('button', { name: 'Create Account' }).click();

    // Should redirect back to checkout (saved in sessionStorage)
    await expect(page.getByRole('heading', { name: 'Checkout' })).toBeVisible({ timeout: 10000 });
  });

  test('accessing orders without auth redirects to login', async ({ page }) => {
    await page.goto('/#/orders');
    await expect(page.getByRole('heading', { name: 'Welcome back' })).toBeVisible({ timeout: 5000 });
  });
});
