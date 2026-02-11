import { test, expect } from '@playwright/test';
import { clearState, registerUser, addProductToCart } from './helpers';

test.describe('Checkout & Orders', () => {
  test.beforeEach(async ({ page }) => {
    await clearState(page);
  });

  test('place an order from cart', async ({ page }) => {
    await registerUser(page);
    await addProductToCart(page, 'Mechanical Keyboard');

    await page.goto('/#/checkout');
    await expect(page.getByRole('heading', { name: 'Checkout' })).toBeVisible();

    // Order summary shows item
    await expect(page.getByText(/Mechanical Keyboard x1/)).toBeVisible();

    // Fill shipping form
    await page.getByLabel('Street Address').fill('456 Oak Ave');
    await page.getByLabel('City').fill('Portland');
    await page.getByLabel('State').fill('OR');
    await page.getByLabel('ZIP Code').fill('97201');
    await page.getByRole('button', { name: 'Place Order' }).click();

    // Should redirect to order detail
    await expect(page).toHaveURL(/\/#\/orders\/\w+/, { timeout: 10000 });
    await expect(page.getByText('Order placed successfully!')).toBeVisible();
  });

  test('order appears in order history', async ({ page }) => {
    await registerUser(page);
    await addProductToCart(page, 'USB-C Hub');

    // Place order
    await page.goto('/#/checkout');
    await page.getByLabel('Street Address').fill('789 Pine St');
    await page.getByLabel('City').fill('Seattle');
    await page.getByLabel('State').fill('WA');
    await page.getByLabel('ZIP Code').fill('98101');
    await page.getByRole('button', { name: 'Place Order' }).click();
    await expect(page).toHaveURL(/\/#\/orders\/\w+/, { timeout: 10000 });

    // Go to orders list
    await page.goto('/#/orders');
    await expect(page.getByRole('heading', { name: 'Your Orders' })).toBeVisible();

    // Should see at least one order card
    await expect(page.locator('.order-card').first()).toBeVisible({ timeout: 10000 });
  });

  test('order detail shows status', async ({ page }) => {
    await registerUser(page);
    await addProductToCart(page, 'Desk Mat');

    await page.goto('/#/checkout');
    await page.getByLabel('Street Address').fill('321 Elm Rd');
    await page.getByLabel('City').fill('Denver');
    await page.getByLabel('State').fill('CO');
    await page.getByLabel('ZIP Code').fill('80201');
    await page.getByRole('button', { name: 'Place Order' }).click();

    // Should be on order detail page with status
    await expect(page).toHaveURL(/\/#\/orders\/\w+/, { timeout: 10000 });
    await expect(page.getByText(/Order #/)).toBeVisible();
    await expect(page.getByText('new')).toBeVisible();
  });

  test('cart is cleared after checkout', async ({ page }) => {
    await registerUser(page);
    await addProductToCart(page, 'Monitor Light Bar');

    // Cart should show 1 item
    await expect(page.getByRole('link', { name: /Cart\s*1/ })).toBeVisible();

    await page.goto('/#/checkout');
    await page.getByLabel('Street Address').fill('100 Market St');
    await page.getByLabel('City').fill('Austin');
    await page.getByLabel('State').fill('TX');
    await page.getByLabel('ZIP Code').fill('73301');
    await page.getByRole('button', { name: 'Place Order' }).click();
    await expect(page).toHaveURL(/\/#\/orders\/\w+/, { timeout: 10000 });

    // Navigate to cart to verify empty
    await page.goto('/#/cart');
    await expect(page.getByText('Your cart is empty')).toBeVisible();
  });

  test('checkout without auth redirects to login', async ({ page }) => {
    // Add to cart without being logged in
    await addProductToCart(page, 'Laptop Stand');

    await page.goto('/#/checkout');
    // Should redirect to login
    await expect(page.getByRole('heading', { name: 'Welcome back' })).toBeVisible({ timeout: 5000 });
  });
});
